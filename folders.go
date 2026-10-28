package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/dmikey/away-agent-os/desktop/internal/macvm"
	"github.com/dmikey/away-agent-os/desktop/internal/sharedfolders"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type SelectedFolder struct {
	Token    string `json:"token"`
	Name     string `json:"name"`
	HostPath string `json:"hostPath"`
}

func (a *App) SharedFolders() []sharedfolders.Grant {
	if a.folders == nil {
		return []sharedfolders.Grant{}
	}
	return a.folders.List()
}
func (a *App) PickSharedFolder() (SelectedFolder, error) {
	p, e := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: "Choose a folder to share with Away"})
	if e != nil || p == "" {
		return SelectedFolder{}, e
	}
	var nonce [16]byte
	rand.Read(nonce[:])
	token := hex.EncodeToString(nonce[:])
	a.mu.Lock()
	if a.folderPicks == nil {
		a.folderPicks = map[string]string{}
	}
	a.folderPicks = map[string]string{token: p}
	a.mu.Unlock()
	return SelectedFolder{Token: token, Name: sharedfolders.DefaultName(p), HostPath: p}, nil
}
func (a *App) applyFolders(change func() error) error {
	a.terminalMu.Lock()
	defer a.terminalMu.Unlock()
	if len(a.terminals) > 0 {
		return errors.New("Close and save Terminal before changing folder access")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for id := range a.requests {
		if strings.HasPrefix(id, "vm:") {
			return errors.New("Wait for the current VM run to finish before changing folder access")
		}
	}
	if a.folders == nil {
		return errors.New("Shared folder configuration is unavailable")
	}
	if e := change(); e != nil {
		return e
	}
	a.configureFolderVMs()
	return nil
}
func (a *App) configureFolderVMs() {
	if a.vms == nil || a.folders == nil {
		return
	}
	grants := []macvm.Share{}
	for _, g := range a.folders.List() {
		if g.Available {
			grants = append(grants, macvm.Share{Name: g.Name, Path: g.HostPath, ReadOnly: g.ReadOnly})
		}
	}
	a.vms.SetShares(grants)
}
func (a *App) ShareSelectedFolder(token, name string, readOnly bool) error {
	return a.applyFolders(func() error {
		p := a.folderPicks[token]
		snapshot, e := a.store.Read()
		if e != nil {
			return e
		}
		var current struct {
			Workbench struct {
				FS struct {
					Entries []struct {
						Path    string `json:"path"`
						Mounted bool   `json:"mounted"`
					} `json:"entries"`
				} `json:"fs"`
			} `json:"workbench"`
		}
		json.Unmarshal(snapshot.State, &current)
		for _, entry := range current.Workbench.FS.Entries {
			if !entry.Mounted && (entry.Path == sharedfolders.Prefix || strings.HasPrefix(entry.Path, sharedfolders.Prefix+"/")) {
				return errors.New("A virtual folder named Shared already exists. Rename it in Explorer before adding mounts.")
			}
		}

		if p == "" {
			return errors.New("Choose a folder using the macOS picker first")
		}
		if e := a.folders.Add(name, p, readOnly); e != nil {
			return e
		}
		delete(a.folderPicks, token)
		return nil
	})
}
func (a *App) SetSharedFolderAccess(name string, readOnly bool) error {
	return a.applyFolders(func() error { return a.folders.SetReadOnly(name, readOnly) })
}
func (a *App) UnshareFolder(name string) error {
	return a.applyFolders(func() error { return a.folders.Remove(name) })
}
func (a *App) ReadShared(path string, content bool) (sharedfolders.Listing, error) {
	if a.folders == nil {
		return sharedfolders.Listing{}, errors.New("Shared folders unavailable")
	}
	return a.folders.Read(path, content)
}
func (a *App) WriteShared(path, content, encoding string, expected int64) (sharedfolders.Entry, error) {
	if a.folders == nil {
		return sharedfolders.Entry{}, errors.New("Shared folders unavailable")
	}
	return a.folders.Write(path, content, encoding, expected)
}
func (a *App) MkdirShared(path string) error {
	if a.folders == nil {
		return errors.New("Shared folders unavailable")
	}
	return a.folders.Mkdir(path)
}
