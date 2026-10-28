package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dmikey/away-agent-os/desktop/internal/sharedfolders"
	"strings"
	"time"

	"github.com/dmikey/away-agent-os/desktop/internal/guestproto"
	"github.com/dmikey/away-agent-os/desktop/internal/macvm"
)

// Interactive terminal methods address only the human's isolated VM, never a host process.
func (a *App) OpenTerminal(id string, revision, cols, rows int) error {
	if a.vms == nil {
		return errors.New("Isolated VM unavailable: " + a.vmError)
	}
	if len(id) < 8 || len(id) > 80 {
		return errors.New("Invalid terminal session")
	}
	a.terminalMu.Lock()
	defer a.terminalMu.Unlock()
	if len(a.terminals) > 0 {
		return errors.New("A terminal is already open. Close it before starting another.")
	}
	snapshot, e := a.store.Read()
	if e != nil {
		return e
	}
	if snapshot.Revision != revision {
		return errors.New("Workspace changed while opening the terminal. Try again.")
	}
	var saved struct {
		Workbench struct {
			FS struct {
				Entries []struct {
					Path     string `json:"path"`
					Kind     string `json:"kind"`
					Content  string `json:"content"`
					Encoding string `json:"encoding"`
					Trashed  string `json:"trashed"`
				} `json:"entries"`
			} `json:"fs"`
		} `json:"workbench"`
	}
	if json.Unmarshal(snapshot.State, &saved) != nil {
		return errors.New("Invalid saved workspace")
	}
	job := guestproto.Job{ID: id, Kind: "shell"}
	for _, f := range saved.Workbench.FS.Entries {
		if f.Trashed == "" && !(len(a.SharedFolders()) > 0 && (f.Path == sharedfolders.Prefix || strings.HasPrefix(f.Path, sharedfolders.Prefix+"/"))) {
			job.Files = append(job.Files, guestproto.File{Path: f.Path, Kind: f.Kind, Content: f.Content, Encoding: f.Encoding})
		}
	}
	ctx, cancel := context.WithTimeout(a.ctx, 45*time.Second)
	defer cancel()
	t, e := a.vms.OpenTerminal(ctx, "you", job, uint16(max(20, min(300, cols))), uint16(max(5, min(120, rows))))
	if e != nil {
		return e
	}
	if a.terminals == nil {
		a.terminals = map[string]*macvm.Terminal{}
	}
	a.terminals[id] = t
	return nil
}
func (a *App) terminal(id string) (*macvm.Terminal, error) {
	a.terminalMu.Lock()
	defer a.terminalMu.Unlock()
	t := a.terminals[id]
	if t == nil {
		return nil, errors.New("Terminal session is closed")
	}
	return t, nil
}
func (a *App) ReadTerminal(id string, offset int64) (macvm.TerminalOutput, error) {
	t, e := a.terminal(id)
	if e != nil {
		return macvm.TerminalOutput{}, e
	}
	return t.Read(offset), nil
}
func (a *App) WriteTerminal(id, data string) error {
	t, e := a.terminal(id)
	if e != nil {
		return e
	}
	return t.Write(data)
}
func (a *App) ResizeTerminal(id string, cols, rows int) error {
	t, e := a.terminal(id)
	if e != nil {
		return e
	}
	return t.Resize(uint16(max(20, min(300, cols))), uint16(max(5, min(120, rows))))
}
func (a *App) CloseTerminal(id string) (guestproto.Result, error) {
	t, e := a.terminal(id)
	if e != nil {
		return guestproto.Result{}, e
	}
	result, e := t.Close()
	a.terminalMu.Lock()
	delete(a.terminals, id)
	a.terminalMu.Unlock()
	return result, e
}
