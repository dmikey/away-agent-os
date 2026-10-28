package main

import (
	"context"
	"github.com/dmikey/away-agent-os/desktop/internal/sharedfolders"
	"github.com/dmikey/away-agent-os/desktop/internal/state"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnlyPickedFoldersCanBeShared(t *testing.T) {
	r, _ := sharedfolders.New(filepath.Join(t.TempDir(), "folders.json"))
	a := &App{folders: r, store: state.New(filepath.Join(t.TempDir(), "workspace.json")), requests: map[string]context.CancelFunc{}, folderPicks: map[string]string{"picked": t.TempDir()}}
	if e := a.ShareSelectedFolder("arbitrary", "project", true); e == nil {
		t.Fatal("unpicked folder accepted")
	}
	if e := a.ShareSelectedFolder("picked", "project", true); e != nil {
		t.Fatal(e)
	}
	if len(a.SharedFolders()) != 1 {
		t.Fatal("folder not registered")
	}
	if e := a.ShareSelectedFolder("picked", "other", true); e == nil {
		t.Fatal("picker token reused")
	}
	a.requests["vm:active"] = func() {}
	if e := a.UnshareFolder("project"); e == nil || !strings.Contains(e.Error(), "Wait") {
		t.Fatal("active run interrupted", e)
	}
	delete(a.requests, "vm:active")
	if e := a.UnshareFolder("project"); e != nil {
		t.Fatal(e)
	}
}
func TestExistingSharedFolderIsPreserved(t *testing.T) {
	r, _ := sharedfolders.New(filepath.Join(t.TempDir(), "folders.json"))
	s := state.New(filepath.Join(t.TempDir(), "workspace.json"))
	s.Commit(-1, `{"windows":{},"workbench":{"fs":{"entries":[{"path":"/workspace/Shared","kind":"directory"}]}}}`)
	a := &App{folders: r, store: s, folderPicks: map[string]string{"picked": t.TempDir()}, requests: map[string]context.CancelFunc{}}
	if e := a.ShareSelectedFolder("picked", "project", true); e == nil {
		t.Fatal("existing virtual Shared folder obscured")
	}
	if len(a.SharedFolders()) != 0 {
		t.Fatal("grant persisted despite collision")
	}
}
