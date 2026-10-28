//go:build darwin && browsercheck

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
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func check() error {
	m, e := macvm.New()
	if e != nil {
		return e
	}
	defer m.Close()
	folder, e := os.MkdirTemp("", "away-browser-share-canary-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(folder)
	if e = os.WriteFile(folder+"/canary.txt", []byte("HOST_UNCHANGED"), 0600); e != nil {
		return e
	}
	m.SetShares([]macvm.Share{{Name: "Canary", Path: folder}})
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if _, e = m.BrowserAction(ctx, "you", guestproto.BrowserAction{Kind: "navigate", URL: "file:///etc/passwd"}); e == nil {
		return fmt.Errorf("file navigation accepted")
	}
	fmt.Println("FILE_NAVIGATION_DENIED")
	f, e := m.BrowserAction(ctx, "you", guestproto.BrowserAction{Kind: "frame"})
	if e != nil {
		return e
	}
	if !f.Sandboxed || len(f.JPEG) < 100 {
		return fmt.Errorf("invalid blank browser frame")
	}
	fmt.Println("CHROMIUM_VM_RENDERER_SANDBOX_OK", f.Width, f.Height)
	if e = m.CheckBrowserFixture(ctx); e != nil {
		return e
	}
	if e = m.Gateway.SetPolicy("you", []string{"example.com"}); e != nil {
		return e
	}
	f, e = m.BrowserAction(ctx, "you", guestproto.BrowserAction{Kind: "navigate", URL: "https://example.com/"})
	if e != nil {
		return e
	}
	for i := 0; i < 15 && !strings.Contains(strings.ToLower(f.Title), "example"); i++ {
		time.Sleep(300 * time.Millisecond)
		f, e = m.BrowserAction(ctx, "you", guestproto.BrowserAction{Kind: "frame"})
		if e != nil {
			return e
		}
	}
	if !strings.Contains(strings.ToLower(f.Title), "example") {
		return fmt.Errorf("expected website title: %s", f.Title)
	}
	fmt.Println("REAL_WEBSITE_RENDER_AND_DOM_OK", len(f.JPEG))
	data, e := os.ReadFile(folder + "/canary.txt")
	if e != nil || string(data) != "HOST_UNCHANGED" {
		return fmt.Errorf("host canary changed")
	}
	fmt.Println("BROWSER_NO_MOUNTS_NO_ADAPTER_UID_2000_HOST_CANARY_OK")
	return nil
}
