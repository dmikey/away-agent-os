//go:build darwin

package macvm

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/dmikey/away-agent-os/desktop/internal/guestproto"
	"strings"
	"time"
)

// CheckTerminal exercises the actual guest PTY. Only explicit developer smoke tests call it.
func (m *Manager) CheckTerminal(ctx context.Context) error {
	t, e := m.OpenTerminal(ctx, "terminal-check", guestproto.Job{ID: "pty-check", Kind: "shell", Files: []guestproto.File{{Path: "/workspace", Kind: "directory", Encoding: "utf8"}}}, 80, 24)
	if e != nil {
		return e
	}
	defer t.Close()
	offset := int64(0)
	output := ""
	wait := func(want string) error {
		for until := time.Now().Add(10 * time.Second); time.Now().Before(until); {
			r := t.Read(offset)
			offset = r.Offset
			b, _ := base64.StdEncoding.DecodeString(r.Data)
			output += string(b)
			if strings.Contains(output, want) {
				output = ""
				return nil
			}
			if r.Closed {
				return fmt.Errorf("terminal exited: %s", r.Error)
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			time.Sleep(20 * time.Millisecond)
		}
		return fmt.Errorf("PTY did not produce %q; output: %q", want, output)
	}
	send := func(s string) error { return t.Write(base64.StdEncoding.EncodeToString([]byte(s))) }
	if e = wait("$ "); e != nil {
		return e
	}
	send("test -t 0 && test -t 1 && test $(id -u) = 1000 && printf '\\nPTY_TTY_OK\\n'\r")
	if e = wait("\r\nPTY_TTY_OK\r\n"); e != nil {
		return e
	}
	fmt.Println("PTY_TTY_AND_UNPRIVILEGED_UID_OK")
	send("export AWAY_PTY_VALUE=42; cd /tmp\r")
	send("printf '\\nSTATE_%s_%s\\n' \"$AWAY_PTY_VALUE\" \"$PWD\"\r")
	if e = wait("\r\nSTATE_42_/tmp\r\n"); e != nil {
		return e
	}
	fmt.Println("PTY_PERSISTENT_SHELL_OK")
	t.Resize(101, 33)
	send("stty size\r")
	if e = wait("\r\n33 101\r\n"); e != nil {
		return e
	}
	fmt.Println("PTY_RESIZE_OK")
	send("read -p 'Name? ' answer; printf '\\nINPUT_%s\\n' \"$answer\"\r")
	if e = wait("Name? "); e != nil {
		return e
	}
	send("away\r")
	if e = wait("\r\nINPUT_away\r\n"); e != nil {
		return e
	}
	fmt.Println("PTY_INTERACTIVE_STDIN_OK")
	send("printf '\\nSLEEPING\\n'; sleep 30\r")
	if e = wait("\r\nSLEEPING\r\n"); e != nil {
		return e
	}
	send("\x03")
	if e = wait("$ "); e != nil {
		return e
	}
	send("printf '\\nINTERRUPT_OK\\n'\r")
	if e = wait("\r\nINTERRUPT_OK\r\n"); e != nil {
		return e
	}
	fmt.Println("PTY_CTRL_C_OK")
	send("test ! -e /Users && printf 'terminal file' > /workspace/terminal-check.txt; printf '\\nFILE_OK\\n'\r")
	if e = wait("\r\nFILE_OK\r\n"); e != nil {
		return e
	}
	result, e := t.Close()
	if e != nil {
		return e
	}
	for _, f := range result.Files {
		if f.Path == "/workspace/terminal-check.txt" {
			b, _ := base64.StdEncoding.DecodeString(f.Content)
			if string(b) == "terminal file" {
				fmt.Println("PTY_WORKSPACE_EXPORT_AND_HOST_ISOLATION_OK")
				return nil
			}
		}
	}
	return errors.New("terminal file was not exported")
}
