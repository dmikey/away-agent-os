package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"net/url"
	"os"
	"path/filepath"

	"strings"
	"sync"
	"time"

	"encoding/base64"
	"github.com/dmikey/away-agent-os/desktop/internal/credentials"
	"github.com/dmikey/away-agent-os/desktop/internal/gateway"
	"github.com/dmikey/away-agent-os/desktop/internal/guestproto"
	"github.com/dmikey/away-agent-os/desktop/internal/macvm"
	"github.com/dmikey/away-agent-os/desktop/internal/provider"
	"github.com/dmikey/away-agent-os/desktop/internal/sharedfolders"
	"github.com/dmikey/away-agent-os/desktop/internal/state"
	"unicode/utf8"
)

// App exposes narrow native bindings only to the bundled frontend. It never loads remote UI.
type App struct {
	folders     *sharedfolders.Registry
	folderPicks map[string]string
	terminalMu  sync.Mutex
	terminals   map[string]*macvm.Terminal
	store       *state.Store
	vms         *macvm.Manager
	vmError     string
	ctx         context.Context
	mu          sync.Mutex
	requests    map[string]context.CancelFunc
}

func newApp() (*App, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	if override := os.Getenv("AWAY_DATA_DIR"); override != "" {
		dir = override
	}
	vms, vmErr := macvm.New()
	reason := ""
	if vmErr != nil {
		reason = vmErr.Error()
	}
	folders, _ := sharedfolders.New(filepath.Join(dir, "Away OS", "shared-folders.json"))
	app := &App{folders: folders, store: state.New(filepath.Join(dir, "Away OS", "workspace.json")), vms: vms, vmError: reason, requests: map[string]context.CancelFunc{}}
	app.configureFolderVMs()
	return app, nil
}
func (a *App) startup(ctx context.Context) { a.ctx = ctx }
func (a *App) shutdown(context.Context) {
	a.mu.Lock()
	for _, cancel := range a.requests {
		cancel()
	}
	a.mu.Unlock()
	a.terminalMu.Lock()
	for _, t := range a.terminals {
		t.Close()
	}
	a.terminalMu.Unlock()
	if a.vms != nil {
		a.vms.Close()
	}
}
func (a *App) ReadWorkspace() (state.Snapshot, error) { return a.store.Read() }
func (a *App) CommitWorkspace(expected int, raw string) (state.Snapshot, error) {
	var candidate struct {
		Workbench struct {
			Runs []struct {
				Status           string `json:"status"`
				ExecutionBackend string `json:"executionBackend"`
				SandboxActor     string `json:"sandboxActor"`
			} `json:"runs"`
		} `json:"workbench"`
	}
	if json.Unmarshal([]byte(raw), &candidate) != nil {
		return state.Snapshot{}, errors.New("Invalid workspace")
	}
	for _, run := range candidate.Workbench.Runs {
		if run.Status == "queued" || run.Status == "running" {
			if a.vms == nil || run.ExecutionBackend != "apple-vz" || run.SandboxActor == "" {
				return state.Snapshot{}, errors.New("Isolated macOS sandbox unavailable or run lacks an isolation policy; execution denied")
			}
		}
	}
	return a.store.Commit(expected, raw)
}

type SandboxInfo struct {
	Backend             string `json:"backend"`
	Available           bool   `json:"available"`
	NativeProcessAccess bool   `json:"nativeProcessAccess"`
	HostMounts          bool   `json:"hostMounts"`
	Fallback            bool   `json:"fallback"`
	Reason              string `json:"reason"`
}

func (a *App) SandboxStatus() SandboxInfo {
	reason := "Dedicated Linux VM per actor. Only folders explicitly shared by you can be mounted under /workspace/Shared. No native shell, host process-control tools or network adapter. Network access goes through a default-deny Go proxy with per-actor domain rules. Code and notebooks share that actor's guest Python kernel."
	if a.vms == nil {
		reason = "Sandbox unavailable: " + a.vmError + ". Execution is disabled."
	}
	return SandboxInfo{Backend: "apple-vz", Available: a.vms != nil, HostMounts: len(a.SharedFolders()) > 0, Reason: reason}
}
func (a *App) ResetSandboxes() {
	a.mu.Lock()
	for id, cancel := range a.requests {
		if strings.HasPrefix(id, "vm:") {
			cancel()
		}
	}
	a.mu.Unlock()
	if a.vms != nil {
		a.vms.Reset()
	}
}
func (a *App) ExecuteRun(id, owner string) (guestproto.Result, error) {
	if a.vms == nil {
		return guestproto.Result{}, errors.New("Isolated VM runtime unavailable: " + a.vmError)
	}
	snapshot, e := a.store.Read()
	if e != nil {
		return guestproto.Result{}, e
	}
	var state struct {
		Workbench struct {
			Runs []struct {
				ID               string `json:"id"`
				Owner            string `json:"owner"`
				Status           string `json:"status"`
				Source           string `json:"source"`
				Language         string `json:"language"`
				FilePath         string `json:"filePath"`
				SandboxActor     string `json:"sandboxActor"`
				ExecutionBackend string `json:"executionBackend"`
				FSBase           struct {
					Entries []struct {
						Path     string `json:"path"`
						Kind     string `json:"kind"`
						Content  string `json:"content"`
						Encoding string `json:"encoding"`
						Trashed  string `json:"trashed"`
					} `json:"entries"`
				} `json:"fsBase"`
			} `json:"runs"`
		} `json:"workbench"`
	}
	if json.Unmarshal(snapshot.State, &state) != nil {
		return guestproto.Result{}, errors.New("Invalid saved workspace")
	}
	for _, run := range state.Workbench.Runs {
		if run.ID != id {
			continue
		}
		if run.Owner != owner || run.Status != "running" || run.ExecutionBackend != "apple-vz" {
			return guestproto.Result{}, errors.New("Run is not authorized for isolated execution")
		}
		kind := "python"
		if run.Language == "shell" {
			kind = "shell"
		}
		job := guestproto.Job{ID: id, Kind: kind, Source: run.Source, FilePath: run.FilePath}
		for _, f := range run.FSBase.Entries {
			if f.Trashed == "" && !(len(a.SharedFolders()) > 0 && (f.Path == sharedfolders.Prefix || strings.HasPrefix(f.Path, sharedfolders.Prefix+"/"))) {
				job.Files = append(job.Files, guestproto.File{Path: f.Path, Kind: f.Kind, Content: f.Content, Encoding: f.Encoding})
			}
		}
		ctx, cancel := context.WithTimeout(a.ctx, 120*time.Second)
		defer cancel()
		key := "vm:" + id
		a.mu.Lock()
		if _, exists := a.requests[key]; exists {
			a.mu.Unlock()
			return guestproto.Result{}, errors.New("Run is already executing")
		}
		a.requests[key] = cancel
		a.mu.Unlock()
		defer func() { a.mu.Lock(); delete(a.requests, key); a.mu.Unlock() }()
		result, e := a.vms.Execute(ctx, run.SandboxActor, job)
		if e != nil {
			return result, e
		}
		if !result.FilesComplete {
			result.Files = nil
			return result, nil
		}
		// Send only changed files. Unchanged text keeps its original encoding and collaborative version.
		before := map[string]guestproto.File{}
		for _, f := range job.Files {
			before[f.Path] = f
		}
		seen := map[string]bool{}
		changes := []guestproto.File{}
		for _, f := range result.Files {
			seen[f.Path] = true
			b, ok := before[f.Path]
			if f.Kind == "directory" {
				if !ok {
					changes = append(changes, f)
				}
				continue
			}
			raw, e := base64.StdEncoding.DecodeString(f.Content)
			if e != nil {
				return guestproto.Result{}, errors.New("Invalid guest file encoding")
			}
			old := []byte(b.Content)
			if b.Encoding == "base64" {
				old, _ = base64.StdEncoding.DecodeString(b.Content)
			}
			if ok && b.Kind == f.Kind && string(raw) == string(old) {
				continue
			}
			if utf8.Valid(raw) && !strings.ContainsRune(string(raw), '\x00') {
				f.Content = string(raw)
				f.Encoding = "utf8"
			}
			changes = append(changes, f)
		}
		for path, f := range before {
			if path != "/workspace" && !seen[path] {
				f.Deleted = true
				f.Content = ""
				changes = append(changes, f)
			}
		}
		result.Files = changes
		return result, nil
	}
	return guestproto.Result{}, errors.New("Run not found")
}

// Boot acknowledgment for isolated launch checks; never logs workspace contents or keys.
func (a *App) FrontendReady() {
	if os.Getenv("AWAY_SHARES_SMOKE_TEST") == "1" {
		go func() {
			defer wailsruntime.Quit(a.ctx)
			ctx, cancel := context.WithTimeout(a.ctx, 90*time.Second)
			defer cancel()
			if a.vms == nil {
				fmt.Println("SHARES_FAILED", a.vmError)
				return
			}
			if e := a.vms.CheckShares(ctx); e != nil {
				fmt.Println("SHARES_FAILED", e)
			} else {
				fmt.Println("SHARES_READY")
			}
		}()
		return
	}

	if os.Getenv("AWAY_TERMINAL_SMOKE_TEST") == "1" {
		go func() {
			defer wailsruntime.Quit(a.ctx)
			ctx, cancel := context.WithTimeout(a.ctx, 90*time.Second)
			defer cancel()
			if a.vms == nil {
				fmt.Println("AWAY_TERMINAL_FAILED:", a.vmError)
				return
			}
			if e := a.vms.CheckTerminal(ctx); e != nil {
				fmt.Println("AWAY_TERMINAL_FAILED:", e)
			} else {
				fmt.Println("AWAY_TERMINAL_READY")
			}
		}()
		return
	}
	if os.Getenv("AWAY_VM_SMOKE_TEST") == "1" {
		go func() {
			defer wailsruntime.Quit(a.ctx)
			ctx, cancel := context.WithTimeout(a.ctx, 45*time.Second)
			defer cancel()
			if a.vms == nil {
				fmt.Println("AWAY_VM_FAILED:", a.vmError)
				return
			}
			r, e := a.vms.Execute(ctx, "you", guestproto.Job{ID: "native-smoke", Kind: "shell", Source: "uname -s; pwd"})
			if e != nil {
				fmt.Println("AWAY_VM_FAILED:", e)
			} else {
				fmt.Println("AWAY_VM_RESULT:", r.Output)
			}
		}()
		return
	}

	if os.Getenv("AWAY_SMOKE_TEST") == "1" {
		fmt.Println("AWAY_NATIVE_READY: React mounted, Go bindings connected, local workspace loaded; host execution disabled")
		wailsruntime.Quit(a.ctx)
	}
}
func (a *App) CancelRequest(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if cancel := a.requests[id]; cancel != nil {
		cancel()
	}
}

// Provider credentials remain in Keychain; only connection metadata reaches React.
func (a *App) ProviderStatus() map[string]bool {
	return (provider.Client{Vault: credentials.Keychain{}}).Status()
}
func (a *App) ConnectProvider(name, key string) (provider.Connection, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	return (provider.Client{Vault: credentials.Keychain{}}).Connect(ctx, name, key)
}

// Complete preserves compatibility with earlier bundled clients.
func (a *App) Complete(id, key, body string) (provider.Response, error) {
	return a.CompleteProvider(id, "OpenAI", key, body)
}
func (a *App) CompleteProvider(id, name, key, body string) (provider.Response, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 45*time.Second)
	defer cancel()
	a.mu.Lock()
	if _, exists := a.requests[id]; exists {
		a.mu.Unlock()
		return provider.Response{}, errors.New("Duplicate request")
	}
	a.requests[id] = cancel
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.requests, id); a.mu.Unlock() }()
	return (provider.Client{Vault: credentials.Keychain{}}).Complete(ctx, name, key, body)
}

// Network policy is a human-facing native setting; no agent tool can change it.
func (a *App) NetworkPolicies() map[string][]string {
	if a.vms == nil {
		return map[string][]string{}
	}
	return a.vms.Gateway.Policies()
}
func (a *App) SetNetworkPolicy(actor string, domains []string) error {
	if a.vms == nil {
		return errors.New("Sandbox unavailable")
	}
	return a.vms.Gateway.SetPolicy(actor, domains)
}
func (a *App) NetworkEvents() []gateway.Event {
	if a.vms == nil {
		return []gateway.Event{}
	}
	return a.vms.Gateway.Events()
}

func (a *App) FetchWeb(actor, url string, maxChars int) (gateway.Fetched, error) {
	if a.vms == nil {
		return gateway.Fetched{}, errors.New("Network gateway unavailable")
	}
	return a.vms.Gateway.Fetch(a.ctx, actor, url, maxChars)
}

func (a *App) OpenExternal(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || len(raw) > 2000 {
		return errors.New("Only HTTPS web links can open externally")
	}
	wailsruntime.BrowserOpenURL(a.ctx, u.String())
	return nil
}
