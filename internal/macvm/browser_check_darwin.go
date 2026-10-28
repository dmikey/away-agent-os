//go:build darwin && browsercheck

package macvm

import (
	"context"
	"fmt"
	"github.com/dmikey/away-agent-os/desktop/internal/guestproto"
	"net/http"
	"strings"
	"time"
)

// Compiled only into the explicitly tagged smoke binary, never shipped in Away.
func (m *Manager) browserProxyHandler(actor string) http.Handler {
	next := m.Gateway.BrowserHandler(actor)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Hostname() == "example.com" && r.URL.Path == "/away-check" && r.Method == "GET" {
			approved := false
			for _, d := range m.Gateway.Policies()[actor] {
				if d == "example.com" {
					approved = true
				}
			}
			if !approved {
				http.Error(w, "denied", 403)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<!doctype html><title>Away browser fixture</title><style>body{margin:0}input,button{position:absolute;left:20px;width:200px;height:40px;box-sizing:border-box}input{top:20px}button{top:80px}</style><input aria-label="Name" oninput="document.querySelector('p').textContent='TYPED '+this.value"><button onclick="document.querySelector('p').textContent='CLICKED '+document.querySelector('input').value">Update</button><p style="position:absolute;top:140px">JS waiting</p><script>document.querySelector('p').textContent='SCRIPT_EXECUTED';</script>`)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (m *Manager) CheckBrowserFixture(ctx context.Context) error {
	if e := m.Gateway.SetPolicy("you", []string{"example.com"}); e != nil {
		return e
	}
	f, e := m.BrowserAction(ctx, "you", guestproto.BrowserAction{Kind: "navigate", URL: "http://example.com/away-check"})
	if e != nil {
		return e
	}
	for i := 0; i < 20 && !strings.Contains(f.Text, "SCRIPT_EXECUTED"); i++ {
		time.Sleep(100 * time.Millisecond)
		f, e = m.BrowserAction(ctx, "you", guestproto.BrowserAction{Kind: "frame"})
		if e != nil {
			return e
		}
	}
	if !f.Sandboxed || !strings.Contains(f.Text, "SCRIPT_EXECUTED") || len(f.JPEG) < 100 {
		return fmt.Errorf("fixture script or image failed: title=%s events=%v", f.Title, m.Gateway.Events())
	}
	fmt.Println("VM_JAVASCRIPT_AND_JPEG_OK")
	for _, a := range []guestproto.BrowserAction{{Kind: "click", X: 50, Y: 35}, {Kind: "type", Text: "Away"}, {Kind: "click", X: 50, Y: 95}} {
		f, e = m.BrowserAction(ctx, "you", a)
		if e != nil {
			return e
		}
	}
	f, e = m.BrowserAction(ctx, "you", guestproto.BrowserAction{Kind: "read"})
	if e != nil {
		return e
	}
	if !strings.Contains(f.Text, "CLICKED Away") {
		return fmt.Errorf("interaction failed: %s", f.Text)
	}
	fmt.Println("VM_CLICK_TYPE_AND_LIVE_DOM_OK")
	m.CloseBrowser("you")
	return nil
}

// The fixture is HTTP; prevent Chromium upgrading it to the real HTTPS site.
func browserKernelArgs() string { return " away_browser_fixture=1" }
