//go:build linux

// This binary is /init inside the Linux VM. It is never compiled into or run on the Mac host.
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/dmikey/away-agent-os/desktop/internal/guestproto"
	"github.com/mdlayher/vsock"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

func fatal(e error) {
	fmt.Fprintln(os.Stderr, e)
	for {
		time.Sleep(time.Hour)
	}
}
func main() {
	for _, d := range []string{"/proc", "/sys", "/dev", "/tmp", "/workspace", "/run"} {
		os.MkdirAll(d, 0755)
	}
	syscall.Mount("proc", "/proc", "proc", syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_NOEXEC, "")
	syscall.Mount("sysfs", "/sys", "sysfs", syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_NOEXEC, "")
	syscall.Mount("devtmpfs", "/dev", "devtmpfs", syscall.MS_NOSUID, "")
	syscall.Mount("tmpfs", "/tmp", "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, "size=128m,mode=1777")
	syscall.Mount("tmpfs", "/workspace", "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, "size=64m,mode=0755,uid=1000,gid=1000")
	syscall.Setrlimit(syscall.RLIMIT_NOFILE, &syscall.Rlimit{Cur: 256, Max: 256})
	// Modules and these fixed commands execute only within the Linux guest.
	for _, m := range []string{"virtio_pci", "virtio_console", "vmw_vsock_virtio_transport"} {
		exec.Command("/bin/busybox", "modprobe", m).Run()
	}
	os.Chown("/workspace", 1000, 1000)
	exec.Command("/bin/busybox", "ip", "link", "set", "lo", "up").Run()
	go networkProxy()
	l, e := vsock.Listen(guestproto.Port, nil)
	if e != nil {
		fatal(e)
	}
	fmt.Println("AWAY_GUEST_READY linux/vsock")
	for {
		c, e := l.Accept()
		if e != nil {
			continue
		}
		func() {
			defer c.Close()
			var job guestproto.Job
			if e := json.NewDecoder(io.LimitReader(c, guestproto.MaxMessage)).Decode(&job); e != nil {
				return
			}
			result := guestproto.Result{ID: job.ID, Identity: "away-linux-vm/v1"}
			if e := guestproto.Validate(job); e != nil {
				result.Error = true
				result.Output = e.Error()
			} else {
				result = execute(job)
			}
			json.NewEncoder(c).Encode(result)
		}()
	}
}

var kernel *exec.Cmd
var kernelIn io.WriteCloser
var kernelOut *bufio.Reader

func resetKernel() {
	if kernel != nil {
		syscall.Kill(-kernel.Process.Pid, syscall.SIGKILL)
		kernel.Wait()
	}
	kernel = nil
}
func startKernel() error {
	if kernel != nil {
		return nil
	}
	kernel = exec.Command("/opt/python/bin/python3", "-u", "/agent_kernel.py")
	kernel.Dir = "/workspace"
	kernel.Env = []string{"PATH=/opt/python/bin:/usr/bin:/bin", "HOME=/workspace", "TMPDIR=/tmp", "PYTHONUNBUFFERED=1", "PYTHONDONTWRITEBYTECODE=1", "HTTP_PROXY=http://127.0.0.1:8080", "HTTPS_PROXY=http://127.0.0.1:8080", "http_proxy=http://127.0.0.1:8080", "https_proxy=http://127.0.0.1:8080"}
	kernel.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 1000, Gid: 1000}, Setpgid: true}
	var e error
	kernelIn, e = kernel.StdinPipe()
	if e != nil {
		return e
	}
	out, e := kernel.StdoutPipe()
	if e != nil {
		return e
	}
	kernelOut = bufio.NewReaderSize(out, 65536)
	kernel.Stderr = os.Stderr
	if e = kernel.Start(); e != nil {
		kernel = nil
	}
	return e
}
func execute(job guestproto.Job) guestproto.Result {
	r := guestproto.Result{ID: job.ID, Identity: "away-linux-vm/v1"}
	// Workspace snapshots replace only guest /workspace; no host directory is mounted.
	children, _ := os.ReadDir("/workspace")
	for _, child := range children {
		os.RemoveAll(filepath.Join("/workspace", child.Name()))
	}
	for _, f := range job.Files {
		if f.Path == "/workspace" {
			continue
		}
		os.MkdirAll(filepath.Dir(f.Path), 0755)
		if f.Kind == "directory" {
			os.MkdirAll(f.Path, 0755)
			os.Chown(f.Path, 1000, 1000)
			continue
		}
		b := []byte(f.Content)
		if f.Encoding == "base64" {
			var e error
			b, e = base64.StdEncoding.DecodeString(f.Content)
			if e != nil {
				r.Error = true
				r.Output = "Invalid workspace encoding"
				return r
			}
		}
		os.Remove(f.Path)
		if e := os.WriteFile(f.Path, b, 0644); e != nil {
			r.Error = true
			r.Output = e.Error()
			return r
		}
		os.Chown(f.Path, 1000, 1000)
	}
	// Do not follow guest symlinks when importing files or collecting outputs.
	if job.Kind == "python" {
		if e := startKernel(); e != nil {
			r.Error = true
			r.Output = e.Error()
			return r
		}
		json.NewEncoder(kernelIn).Encode(map[string]string{"source": job.Source, "filePath": job.FilePath})
		ch := make(chan []byte, 1)
		go func() {
			line, _, e := kernelOut.ReadLine()
			if e != nil {
				ch <- nil
			} else {
				ch <- line
			}
		}()
		select {
		case b := <-ch:
			var v struct {
				Output string `json:"output"`
				Error  bool   `json:"error"`
			}
			if b == nil || json.Unmarshal(b, &v) != nil {
				resetKernel()
				r.Error = true
				r.Output = "Guest Python kernel stopped"
			} else {
				r.Output = v.Output
				r.Error = v.Error
			}
		case <-time.After(85 * time.Second):
			resetKernel()
			r.Error = true
			r.Output = "Execution timed out; guest kernel reset"
		}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 85*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", job.Source)
		cmd.Dir = "/workspace"
		cmd.Env = []string{"PATH=/opt/python/bin:/usr/bin:/bin", "HOME=/workspace", "TMPDIR=/tmp", "HTTP_PROXY=http://127.0.0.1:8080", "HTTPS_PROXY=http://127.0.0.1:8080", "http_proxy=http://127.0.0.1:8080", "https_proxy=http://127.0.0.1:8080"}
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 1000, Gid: 1000}, Setpgid: true}
		out := &limitedOutput{}
		cmd.Stdout = out
		cmd.Stderr = out
		r.Error = cmd.Run() != nil
		r.Output = string(out.data)
	}
	total := 0
	exportFailed := false
	filepath.WalkDir("/workspace", func(p string, d os.DirEntry, e error) error {
		if e != nil || p == "/workspace" {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if len(r.Files) >= 256 {
			exportFailed = true
			return filepath.SkipAll
		}
		if d.IsDir() {
			r.Files = append(r.Files, guestproto.File{Path: p, Kind: "directory", Encoding: "utf8"})
			return nil
		}
		info, e := d.Info()
		if e != nil || !info.Mode().IsRegular() || info.Size() > 384000 {
			exportFailed = true
			return nil
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return nil
		}
		content := base64.StdEncoding.EncodeToString(b)
		total += len(content)
		if total > 2400000 {
			exportFailed = true
			return filepath.SkipAll
		}
		r.Files = append(r.Files, guestproto.File{Path: p, Kind: "file", Content: content, Encoding: "base64"})
		return nil
	})
	r.FilesComplete = !exportFailed
	if exportFailed {
		r.Files = nil
		r.Error = true
		r.Output += "\nWorkspace export exceeded limits; no files were synced."
	}
	sort.Slice(r.Files, func(i, j int) bool { return r.Files[i].Path < r.Files[j].Path })
	return r
}

type limitedOutput struct{ data []byte }

func (o *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if len(o.data) < 20000 {
		o.data = append(o.data, p[:min(len(p), 20000-len(o.data))]...)
	}
	return n, nil
}

// Only a loopback HTTP proxy is exposed. The guest has no network adapter.
func networkProxy() {
	listener, e := net.Listen("tcp", "127.0.0.1:8080")
	if e != nil {
		return
	}
	for {
		client, e := listener.Accept()
		if e != nil {
			return
		}
		go func() {
			defer client.Close()
			upstream, e := vsock.Dial(vsock.Host, 1025, nil)
			if e != nil {
				return
			}
			defer upstream.Close()
			client.SetDeadline(time.Now().Add(65 * time.Second))
			upstream.SetDeadline(time.Now().Add(65 * time.Second))
			done := make(chan struct{}, 2)
			go func() { io.Copy(upstream, client); done <- struct{}{} }()
			go func() { io.Copy(client, upstream); done <- struct{}{} }()
			<-done
			client.Close()
			upstream.Close()
		}()
	}
}
