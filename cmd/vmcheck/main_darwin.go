//go:build darwin

package main

import (
	"context"
	"fmt"
	"github.com/dmikey/away-agent-os/desktop/internal/guestproto"
	"github.com/dmikey/away-agent-os/desktop/internal/macvm"
	"os"
	"strings"
	"time"
)

func main() {
	if e := check(); e != nil {
		fmt.Fprintln(os.Stderr, "VM CHECK FAILED:", e)
		os.Exit(1)
	}
}
func check() error {
	if os.Getenv("AWAY_SHARE_CHECK") == "1" {
		m, e := macvm.New()
		if e != nil {
			return e
		}
		defer m.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		return m.CheckShares(ctx)
	}

	canary, e := os.CreateTemp("", "away-host-canary-")
	if e != nil {
		return e
	}
	defer os.Remove(canary.Name())
	canary.WriteString("HOST_UNCHANGED")
	canary.Close()
	manager, e := macvm.New()
	if e != nil {
		return e
	}
	defer manager.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	job := guestproto.Job{ID: "first", Kind: "python", Source: fmt.Sprintf(`import os,platform,socket
assert platform.system() == 'Linux'
assert platform.machine() == 'aarch64'
assert not os.path.exists(%q)
assert not os.path.exists('/Users')
s=socket.socket();s.settimeout(1)
assert s.connect_ex(('1.1.1.1',443)) != 0
s.close()
shared_value=42
open('/workspace/result.txt','w').write('Made inside Linux')
print('LINUX_IDENTITY_OK HOST_CANARY_INACCESSIBLE NETWORK_UNAVAILABLE')`, canary.Name()), Files: []guestproto.File{{Path: "/workspace", Kind: "directory", Encoding: "utf8"}}}
	result, e := manager.Execute(ctx, "milo", job)
	if e != nil {
		return e
	}
	if result.Error || !strings.Contains(result.Output, "LINUX_IDENTITY_OK") {
		return fmt.Errorf("guest check: %s", result.Output)
	}
	fmt.Print(result.Output)
	if !result.FilesComplete || len(result.Files) != 1 || result.Files[0].Path != "/workspace/result.txt" {
		return fmt.Errorf("guest file export missing")
	}
	fmt.Println("WORKSPACE_EXPORT_OK")
	result, e = manager.Execute(ctx, "milo", guestproto.Job{ID: "second", Kind: "python", Source: "assert shared_value==42\nprint('SHARED_KERNEL_OK')"})
	if e != nil || result.Error {
		return fmt.Errorf("persistent kernel: %v %s", e, result.Output)
	}
	fmt.Print(result.Output)
	result, e = manager.Execute(ctx, "fern", guestproto.Job{ID: "separate", Kind: "python", Source: "assert 'shared_value' not in globals()\nprint('SEPARATE_AGENT_VM_OK')"})
	if e != nil || result.Error {
		return fmt.Errorf("agent isolation: %v %s", e, result.Output)
	}
	fmt.Print(result.Output)

	result, e = manager.Execute(ctx, "milo", guestproto.Job{ID: "network-denied", Kind: "python", Source: `import urllib.request,urllib.error
try:
 urllib.request.urlopen('http://example.com/',timeout=10)
 raise AssertionError('unapproved request escaped')
except urllib.error.HTTPError as e:
 assert e.code==403
print('DEFAULT_DENY_PROXY_OK')`})
	if e != nil || result.Error {
		return fmt.Errorf("default-deny proxy: %v %s", e, result.Output)
	}
	fmt.Print(result.Output)
	if e = manager.Gateway.SetPolicy("milo", []string{"example.com"}); e != nil {
		return e
	}
	result, e = manager.Execute(ctx, "milo", guestproto.Job{ID: "network-approved", Kind: "python", Source: `import urllib.request
with urllib.request.urlopen('https://example.com/',timeout=20) as response:
 assert response.status==200
 response.read(200)
print('APPROVED_HTTPS_PROXY_OK')`})
	if e != nil || result.Error {
		return fmt.Errorf("approved HTTPS proxy: %v %s", e, result.Output)
	}
	fmt.Print(result.Output)
	result, e = manager.Execute(ctx, "fern", guestproto.Job{ID: "network-separate", Kind: "python", Source: `import urllib.request,urllib.error
try:
 urllib.request.urlopen('http://example.com/',timeout=10)
 raise AssertionError('another actor inherited policy')
except urllib.error.HTTPError as e:
 assert e.code==403
print('PER_AGENT_NETWORK_POLICY_OK')`})
	if e != nil || result.Error {
		return fmt.Errorf("per-agent proxy: %v %s", e, result.Output)
	}
	fmt.Print(result.Output)
	events := manager.Gateway.Events()
	if len(events) < 3 {
		return fmt.Errorf("network events missing")
	}
	fmt.Println("NETWORK_ACTIVITY_RECORDED")
	short, stop := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer stop()
	_, e = manager.Execute(short, "milo", guestproto.Job{ID: "cancel", Kind: "python", Source: "while True: pass"})
	if e == nil {
		return fmt.Errorf("cancellation failed")
	}
	fmt.Println("VM_CANCELLATION_OK")
	b, _ := os.ReadFile(canary.Name())
	if string(b) != "HOST_UNCHANGED" {
		return fmt.Errorf("host canary changed")
	}
	fmt.Println("HOST_CANARY_UNCHANGED")
	return nil
}
