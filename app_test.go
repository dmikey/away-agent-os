package main

import (
	"github.com/dmikey/away-agent-os/desktop/internal/state"
	"path/filepath"
	"testing"
)

func TestNativeBoundaryRejectsExecution(t *testing.T) {
	a := &App{store: state.New(filepath.Join(t.TempDir(), "workspace.json"))}
	for _, status := range []string{"queued", "running"} {
		if _, err := a.CommitWorkspace(-1, `{"windows":{},"workbench":{"runs":[{"status":"`+status+`"}]}}`); err == nil {
			t.Fatal("native boundary permitted execution:", status)
		}
	}
	info := a.SandboxStatus()
	if info.Available || info.NativeProcessAccess || info.HostMounts || info.Fallback {
		t.Fatal("unsafe execution policy", info)
	}

}
