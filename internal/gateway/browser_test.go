package gateway

import (
	"context"
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBrowserResourcePermission(t *testing.T) {
	p := New()
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.BrowserHandler("milo").ServeHTTP(w, httptest.NewRequest("GET", "http://cdn.example.com/image", nil))
	}()
	var id string
	for until := time.Now().Add(time.Second); time.Now().Before(until); {
		rs := p.AccessRequests()
		if len(rs) > 0 {
			id = rs[0].ID
			break
		}
		time.Sleep(time.Millisecond)
	}
	if id == "" {
		t.Fatal("embedded resource failed to request approval")
	}
	if len(p.Policies()["milo"]) != 0 {
		t.Fatal("resource auto-approved")
	}
	if _, e := p.ResolveAccess(id, false); e != nil {
		t.Fatal(e)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("denial did not resume request")
	}
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	if len(p.Policies()["milo"]) != 0 {
		t.Fatal("denial changed policy")
	}
}
func TestBrowserRejectsPrivateAndPortsBeforePrompt(t *testing.T) {
	p := New()
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	for _, raw := range []string{"http://127.0.0.1/", "http://example.com/", "http://example.com:8080/"} {
		w := httptest.NewRecorder()
		p.BrowserHandler("milo").ServeHTTP(w, httptest.NewRequest("GET", raw, nil))
		if w.Code != 403 {
			t.Fatal(raw, w.Code)
		}
	}
	if len(p.AccessRequests()) != 0 {
		t.Fatal("unsafe target prompted")
	}
}
