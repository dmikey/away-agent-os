package macvm

import (
	"encoding/base64"
	"encoding/json"
	"github.com/dmikey/away-agent-os/desktop/internal/guestproto"
	"net"
	"strings"
	"testing"
	"time"
)

func TestTerminalStream(t *testing.T) {
	host, guest := net.Pipe()
	released := make(chan struct{})
	go func() {
		defer guest.Close()
		d := json.NewDecoder(guest)
		e := json.NewEncoder(guest)
		var f guestproto.TerminalFrame
		d.Decode(&f)
		e.Encode(guestproto.TerminalFrame{Type: "ready"})
		e.Encode(guestproto.TerminalFrame{Type: "output", Data: base64.StdEncoding.EncodeToString([]byte("hello\r\n$ "))})
		for {
			if d.Decode(&f) != nil {
				return
			}
			switch f.Type {
			case "input":
				e.Encode(guestproto.TerminalFrame{Type: "output", Data: f.Data})
			case "resize":
				if f.Cols != 101 || f.Rows != 33 {
					t.Error("bad resize")
				}
			case "close":
				e.Encode(guestproto.TerminalFrame{Type: "exit", Result: &guestproto.Result{ID: "test", Identity: "away-linux-vm/v1", FilesComplete: true}})
				return
			}
		}
	}()
	term, e := newTerminal(host, guestproto.Job{ID: "test", Kind: "shell"}, 80, 24, func() { close(released) })
	if e != nil {
		t.Fatal(e)
	}
	if e = term.Write(base64.StdEncoding.EncodeToString([]byte("\x03"))); e != nil {
		t.Fatal(e)
	}
	if e = term.Resize(101, 33); e != nil {
		t.Fatal(e)
	}
	if e = term.Write(strings.Repeat("!", 32769)); e == nil {
		t.Fatal("invalid input accepted")
	}
	var output []byte
	offset := int64(0)
	for until := time.Now().Add(time.Second); time.Now().Before(until); {
		r := term.Read(offset)
		b, _ := base64.StdEncoding.DecodeString(r.Data)
		output = append(output, b...)
		offset = r.Offset
		if strings.Contains(string(output), "\x03") {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if string(output) != "hello\r\n$ \x03" {
		t.Fatal("output order", string(output))
	}
	if _, e = term.Close(); e != nil {
		t.Fatal(e)
	}
	<-released
}
func TestTerminalHistoryBounded(t *testing.T) {
	term := &Terminal{start: 10, history: []byte("abcdef"), closed: true}
	r := term.Read(0)
	data, _ := base64.StdEncoding.DecodeString(r.Data)
	if !r.Truncated || string(data) != "abcdef" || r.Offset != 16 || !r.Closed {
		t.Fatal(r)
	}
	r = term.Read(100)
	if r.Data != "" || r.Offset != 16 {
		t.Fatal(r)
	}
}
