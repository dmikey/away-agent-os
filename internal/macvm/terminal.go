package macvm

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/dmikey/away-agent-os/desktop/internal/guestproto"
)

const terminalHistory = 512 * 1024

// Terminal contains VM bytes only. It has no host subprocess or filesystem API.
type Terminal struct {
	conn    net.Conn
	writeMu sync.Mutex
	mu      sync.Mutex
	history []byte
	start   int64
	closed  bool
	failure string
	result  *guestproto.Result
	done    chan struct{}
}
type TerminalOutput struct {
	Data      string `json:"data"`
	Offset    int64  `json:"offset"`
	Truncated bool   `json:"truncated"`
	Closed    bool   `json:"closed"`
	Error     string `json:"error"`
}

func newTerminal(conn net.Conn, job guestproto.Job, cols, rows uint16, release func()) (*Terminal, error) {
	t := &Terminal{conn: conn, done: make(chan struct{})}
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	if e := t.send(guestproto.TerminalFrame{Type: "open", Job: &job, Cols: cols, Rows: rows}); e != nil {
		conn.Close()
		release()
		return nil, e
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 4096), guestproto.MaxMessage)
	if !scanner.Scan() {
		conn.Close()
		release()
		return nil, errors.New("The Linux terminal did not become ready")
	}
	var f guestproto.TerminalFrame
	if json.Unmarshal(scanner.Bytes(), &f) != nil || f.Type != "ready" {
		conn.Close()
		release()
		return nil, errors.New("Linux terminal startup failed: " + f.Error)
	}
	conn.SetDeadline(time.Time{})
	go func() {
		defer close(t.done)
		defer conn.Close()
		defer release()
		defer func() { t.mu.Lock(); t.closed = true; t.mu.Unlock() }()
		for scanner.Scan() {
			var f guestproto.TerminalFrame
			if json.Unmarshal(scanner.Bytes(), &f) != nil {
				break
			}
			t.mu.Lock()
			switch f.Type {
			case "output":
				data, e := base64.StdEncoding.DecodeString(f.Data)
				if e != nil || len(data) > 32768 {
					t.failure = "Invalid terminal output"
					t.mu.Unlock()
					return
				}
				t.history = append(t.history, data...)
				if len(t.history) > terminalHistory {
					n := len(t.history) - terminalHistory
					t.start += int64(n)
					t.history = append([]byte(nil), t.history[n:]...)
				}
			case "exit":
				if f.Result != nil && f.Result.ID == job.ID && f.Result.Identity == "away-linux-vm/v1" && f.Result.FilesComplete && guestproto.Validate(guestproto.Job{ID: job.ID, Kind: "shell", Files: f.Result.Files}) == nil {
					t.result = f.Result
				} else {
					t.failure = "Terminal files could not be exported; no files were synced."
				}
				t.mu.Unlock()
				return
			case "error":
				t.failure = f.Error
				t.mu.Unlock()
				return
			}
			t.mu.Unlock()
		}
		t.mu.Lock()
		t.failure = "Terminal disconnected. Unsaved VM files were not exported."
		t.mu.Unlock()
	}()
	return t, nil
}
func (t *Terminal) send(f guestproto.TerminalFrame) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	t.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	e := json.NewEncoder(t.conn).Encode(f)
	t.conn.SetWriteDeadline(time.Time{})
	return e
}
func (t *Terminal) Write(data string) error {
	b, e := base64.StdEncoding.DecodeString(data)
	if e != nil || len(b) > 32768 {
		return errors.New("Terminal input is too large")
	}
	return t.send(guestproto.TerminalFrame{Type: "input", Data: data})
}
func (t *Terminal) Resize(cols, rows uint16) error {
	if cols < 20 || cols > 300 || rows < 5 || rows > 120 {
		return errors.New("Invalid terminal dimensions")
	}
	return t.send(guestproto.TerminalFrame{Type: "resize", Cols: cols, Rows: rows})
}
func (t *Terminal) Read(offset int64) TerminalOutput {
	t.mu.Lock()
	defer t.mu.Unlock()
	truncated := offset < t.start
	if offset < t.start {
		offset = t.start
	}
	end := t.start + int64(len(t.history))
	if offset > end {
		offset = end
	}
	n := min(end-offset, 32768)
	data := t.history[offset-t.start : offset-t.start+n]
	return TerminalOutput{Data: base64.StdEncoding.EncodeToString(data), Offset: offset + n, Truncated: truncated, Closed: t.closed && offset+n == end, Error: t.failure}
}
func (t *Terminal) Close() (guestproto.Result, error) {
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if !closed {
		t.send(guestproto.TerminalFrame{Type: "close"})
	}
	select {
	case <-t.done:
	case <-time.After(5 * time.Second):
		t.conn.Close()
		return guestproto.Result{}, errors.New("Terminal did not close in time. Workspace files were not synced.")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.result != nil {
		return *t.result, nil
	}
	return guestproto.Result{}, errors.New(t.failure)
}
