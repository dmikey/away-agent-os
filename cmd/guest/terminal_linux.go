//go:build linux

package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"net"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"github.com/dmikey/away-agent-os/desktop/internal/guestproto"
	"github.com/mdlayher/vsock"
)

func terminalServer() {
	l, e := vsock.Listen(guestproto.TerminalPort, nil)
	if e != nil {
		return
	}
	for {
		c, e := l.Accept()
		if e != nil {
			return
		}
		go serveTerminal(c)
	}
}
func serveTerminal(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 4096), guestproto.MaxMessage)
	if !scanner.Scan() {
		return
	}
	var start guestproto.TerminalFrame
	if json.Unmarshal(scanner.Bytes(), &start) != nil || start.Type != "open" || start.Job == nil || guestproto.Validate(*start.Job) != nil {
		return
	}
	var writeMu sync.Mutex
	send := func(f guestproto.TerminalFrame) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return json.NewEncoder(conn).Encode(f)
	}
	if !workspaceMu.TryLock() {
		send(guestproto.TerminalFrame{Type: "error", Error: "Another run is active. Try again when it finishes."})
		return
	}
	defer workspaceMu.Unlock()
	initial := *start.Job
	initial.Kind = "shell"
	initial.Source = ":"
	if r := execute(initial); r.Error {
		send(guestproto.TerminalFrame{Type: "error", Error: r.Output})
		return
	}
	cmd := exec.Command("/bin/sh", "-i")
	cmd.Dir = "/workspace"
	cmd.Env = []string{"PATH=/opt/python/bin:/usr/bin:/bin", "HOME=/workspace", "TMPDIR=/tmp", "TERM=xterm-256color", "PS1=\\w $ ", "HTTP_PROXY=http://127.0.0.1:8080", "HTTPS_PROXY=http://127.0.0.1:8080", "http_proxy=http://127.0.0.1:8080", "https_proxy=http://127.0.0.1:8080"}
	master, e := pty.StartWithAttrs(cmd, &pty.Winsize{Cols: max(20, min(300, start.Cols)), Rows: max(5, min(120, start.Rows))}, &syscall.SysProcAttr{Setsid: true, Setctty: true, Credential: &syscall.Credential{Uid: 1000, Gid: 1000}})
	if e != nil {
		send(guestproto.TerminalFrame{Type: "error", Error: "Could not start the Linux PTY: " + e.Error()})
		return
	}
	defer master.Close()
	defer syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if send(guestproto.TerminalFrame{Type: "ready"}) != nil {
		return
	}
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		buf := make([]byte, 8192)
		for {
			n, e := master.Read(buf)
			if n > 0 {
				if send(guestproto.TerminalFrame{Type: "output", Data: base64.StdEncoding.EncodeToString(buf[:n])}) != nil {
					return
				}
			}
			if e != nil {
				return
			}
		}
	}()
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		defer syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)
		for scanner.Scan() {
			var f guestproto.TerminalFrame
			if json.Unmarshal(scanner.Bytes(), &f) != nil {
				return
			}
			switch f.Type {
			case "input":
				data, e := base64.StdEncoding.DecodeString(f.Data)
				if e != nil || len(data) > 32768 {
					return
				}
				if _, e = master.Write(data); e != nil {
					return
				}
			case "resize":
				if f.Cols >= 20 && f.Cols <= 300 && f.Rows >= 5 && f.Rows <= 120 {
					pty.Setsize(master, &pty.Winsize{Cols: f.Cols, Rows: f.Rows})
				}
			case "close":
				return
			default:
				return
			}
		}
	}()
	cmd.Wait()
	master.Close()
	<-outputDone
	result := exportWorkspace(guestproto.Result{ID: initial.ID, Identity: "away-linux-vm/v1"})
	send(guestproto.TerminalFrame{Type: "exit", Result: &result})
	conn.Close()
	<-inputDone
}
