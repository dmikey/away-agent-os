//go:build darwin

package macvm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Code-Hex/vz/v3"
	"github.com/dmikey/away-agent-os/desktop/internal/gateway"
	"github.com/dmikey/away-agent-os/desktop/internal/guestproto"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

type session struct {
	mu            sync.Mutex
	vm            *vz.VirtualMachine
	files         []*os.File
	proxyServer   *http.Server
	proxyListener *vz.VirtioSocketListener
}
type Manager struct {
	mu       sync.Mutex
	dir      string
	sessions map[string]*session
	Gateway  *gateway.Proxy
	shares   []Share
}

func New() (*Manager, error) {
	dir, e := prepareAssets()
	if e != nil {
		return nil, e
	}
	boot, e := vz.NewLinuxBootLoader(filepath.Join(dir, "vmlinuz"), vz.WithInitrd(filepath.Join(dir, "initramfs.gz")))
	if e != nil {
		os.RemoveAll(dir)
		return nil, e
	}
	preflight, e := vz.NewVirtualMachineConfiguration(boot, 2, 512*1024*1024)
	if e != nil {
		os.RemoveAll(dir)
		return nil, e
	}
	if ok, e := preflight.Validate(); !ok || e != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("macOS cannot authorize the isolated VM. Use the signed Away OS.app, rather than an unsigned binary or development build. macOS diagnostic: %v", e)
	}
	return &Manager{dir: dir, sessions: map[string]*session{}, Gateway: gateway.New()}, nil
}
func (m *Manager) session(actor string) (*session, error) {
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(actor) {
		return nil, errors.New("Invalid sandbox actor")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[actor]; s != nil {
		return s, nil
	}
	if len(m.sessions) >= 3 {
		return nil, errors.New("Three agent VMs are already active. Reset sandboxes to free memory")
	}
	cmdline := "console=hvc0 rdinit=/init panic=-1 quiet"
	if len(m.shares) > 0 {
		cmdline += " away_shared=1"
	}
	boot, e := vz.NewLinuxBootLoader(filepath.Join(m.dir, "vmlinuz"), vz.WithInitrd(filepath.Join(m.dir, "initramfs.gz")), vz.WithCommandLine(cmdline))
	if e != nil {
		return nil, e
	}
	config, e := vz.NewVirtualMachineConfiguration(boot, 2, 512*1024*1024)
	if e != nil {
		return nil, e
	}
	socket, e := vz.NewVirtioSocketDeviceConfiguration()
	if e != nil {
		return nil, e
	}
	config.SetSocketDevicesVirtualMachineConfiguration([]vz.SocketDeviceConfiguration{socket})
	entropy, e := vz.NewVirtioEntropyDeviceConfiguration()
	if e != nil {
		return nil, e
	}
	config.SetEntropyDevicesVirtualMachineConfiguration([]*vz.VirtioEntropyDeviceConfiguration{entropy})
	// Only explicitly granted directories are attached. No host disks, sockets or network adapter.
	if len(m.shares) > 0 {
		shared := map[string]*vz.SharedDirectory{}
		for _, grant := range m.shares {
			dir, e := vz.NewSharedDirectory(grant.Path, grant.ReadOnly)
			if e != nil {
				return nil, e
			}
			shared[grant.Name] = dir
		}
		share, e := vz.NewMultipleDirectoryShare(shared)
		if e != nil {
			return nil, e
		}
		device, e := vz.NewVirtioFileSystemDeviceConfiguration("away-shared")
		if e != nil {
			return nil, e
		}
		device.SetDirectoryShare(share)
		config.SetDirectorySharingDevicesVirtualMachineConfiguration([]vz.DirectorySharingDeviceConfiguration{device})
	}

	input, e := os.Open(os.DevNull)
	if e != nil {
		return nil, e
	}
	output, e := os.OpenFile(filepath.Join(m.dir, actor+".boot.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		input.Close()
		return nil, e
	}
	attachment, e := vz.NewFileHandleSerialPortAttachment(input, output)
	if e != nil {
		input.Close()
		output.Close()
		return nil, e
	}
	serial, e := vz.NewVirtioConsoleDeviceSerialPortConfiguration(attachment)
	if e != nil {
		return nil, e
	}
	config.SetSerialPortsVirtualMachineConfiguration([]*vz.VirtioConsoleDeviceSerialPortConfiguration{serial})
	if ok, e := config.Validate(); !ok || e != nil {
		input.Close()
		output.Close()
		if e != nil {
			return nil, fmt.Errorf("Invalid isolated VM configuration: %w", e)
		}
		return nil, errors.New("Invalid isolated VM configuration: macOS rejected the configuration without a diagnostic")
	}
	vm, e := vz.NewVirtualMachine(config)
	if e != nil {
		input.Close()
		output.Close()
		return nil, e
	}
	if e = vm.Start(); e != nil {
		input.Close()
		output.Close()
		return nil, e
	}
	s := &session{vm: vm, files: []*os.File{input, output}}
	listener, e := vm.SocketDevices()[0].Listen(1025)
	if e != nil {
		vm.Stop()
		input.Close()
		output.Close()
		return nil, e
	}
	server := &http.Server{Handler: m.Gateway.Handler(actor), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 16384}
	s.proxyServer = server
	s.proxyListener = listener
	go server.Serve(listener)
	m.sessions[actor] = s
	return s, nil
}
func (m *Manager) drop(actor string, s *session) {
	m.mu.Lock()
	if m.sessions[actor] == s {
		delete(m.sessions, actor)
	}
	m.mu.Unlock()
	if s.proxyServer != nil {
		s.proxyServer.Close()
	}
	if s.proxyListener != nil {
		s.proxyListener.Close()
	}
	s.vm.Stop()
	for _, f := range s.files {
		f.Close()
	}
}
func (m *Manager) Execute(ctx context.Context, actor string, job guestproto.Job) (guestproto.Result, error) {
	if e := guestproto.Validate(job); e != nil {
		return guestproto.Result{}, e
	}
	s, e := m.session(actor)
	if e != nil {
		return guestproto.Result{}, e
	}
	if !s.mu.TryLock() {
		return guestproto.Result{}, errors.New("Close the interactive terminal before running code or notebooks in this VM")
	}
	defer s.mu.Unlock()
	var conn *vz.VirtioSocketConnection
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			m.drop(actor, s)
			return guestproto.Result{}, ctx.Err()
		}
		conn, e = s.vm.SocketDevices()[0].Connect(guestproto.Port)
		if e == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if conn == nil {
		m.drop(actor, s)
		return guestproto.Result{}, errors.New("Linux guest did not become ready; execution denied")
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(90 * time.Second))
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
			m.drop(actor, s)
		case <-done:
		}
	}()
	if e = json.NewEncoder(conn).Encode(job); e != nil {
		m.drop(actor, s)
		return guestproto.Result{}, errors.New("Guest input transfer failed")
	}
	var result guestproto.Result
	if e = json.NewDecoder(io.LimitReader(conn, guestproto.MaxMessage)).Decode(&result); e != nil {
		m.drop(actor, s)
		return result, errors.New("Guest stopped or exceeded its response limit")
	}
	if result.ID != job.ID || result.Identity != "away-linux-vm/v1" || len(result.Output) > 20000 || len(result.Files) > 256 {
		m.drop(actor, s)
		return guestproto.Result{}, errors.New("Invalid guest response")
	}
	// Validate exported paths and quotas as untrusted data before the shared filesystem sees them.
	if e = guestproto.Validate(guestproto.Job{ID: result.ID, Kind: job.Kind, Files: result.Files}); e != nil {
		m.drop(actor, s)
		return guestproto.Result{}, e
	}
	return result, nil
}
func (m *Manager) Reset() {
	m.mu.Lock()
	sessions := m.sessions
	m.sessions = map[string]*session{}
	m.mu.Unlock()
	for actor, s := range sessions {
		m.drop(actor, s)
	}
}
func (m *Manager) Close() { m.Reset(); os.RemoveAll(m.dir) }

// OpenTerminal reserves one actor's VM until the interactive shell exits.
func (m *Manager) OpenTerminal(ctx context.Context, actor string, job guestproto.Job, cols, rows uint16) (*Terminal, error) {
	if e := guestproto.Validate(job); e != nil {
		return nil, e
	}
	s, e := m.session(actor)
	if e != nil {
		return nil, e
	}
	if !s.mu.TryLock() {
		return nil, errors.New("This actor's VM is busy. Close its terminal or wait for the current run.")
	}
	var conn *vz.VirtioSocketConnection
	for until := time.Now().Add(30 * time.Second); time.Now().Before(until); {
		if ctx.Err() != nil {
			s.mu.Unlock()
			return nil, ctx.Err()
		}
		conn, e = s.vm.SocketDevices()[0].Connect(guestproto.TerminalPort)
		if e == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if conn == nil {
		s.mu.Unlock()
		return nil, errors.New("The Linux terminal service did not become ready")
	}
	return newTerminal(conn, job, cols, rows, s.mu.Unlock)
}

func (m *Manager) SetShares(shares []Share) {
	m.Reset()
	m.mu.Lock()
	m.shares = append([]Share(nil), shares...)
	m.mu.Unlock()
}
