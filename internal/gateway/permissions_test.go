package gateway

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func permissionProxy(t *testing.T, file string) *Proxy {
	t.Helper()
	p := New()
	if e := p.ConfigurePermissions(file); e != nil {
		t.Fatal(e)
	}
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}
	return p
}
func TestRememberDomainAndAllPersistWithActorScope(t *testing.T) {
	file := filepath.Join(t.TempDir(), "permissions.json")
	p := permissionProxy(t, file)
	req, e := p.RequestAccess(context.Background(), "donald", "https://example.com/", "Review page")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.ResolveAccessChoice(req.ID, "always"); e != nil {
		t.Fatal(e)
	}
	p = permissionProxy(t, file)
	req, e = p.RequestAccess(context.Background(), "donald", "https://example.com/new", "Read again")
	if e != nil || req.Status != "approved" {
		t.Fatal(req, e)
	}
	req, _ = p.RequestAccess(context.Background(), "marv", "https://example.com/", "Read page")
	if req.Status != "pending" {
		t.Fatal("other actor inherited domain")
	}
	req, _ = p.RequestAccess(context.Background(), "donald", "https://cdn.example.com/", "Load resource")
	if req.Status != "pending" {
		t.Fatal("exact domain became wildcard")
	}
	other, _ := p.RequestAccess(context.Background(), "donald", "https://other.example.com/", "Load another resource")
	if _, e = p.ResolveAccessChoice(req.ID, "all"); e != nil {
		t.Fatal(e)
	}
	resolved, e := p.WaitAccess(context.Background(), other.ID)
	if e != nil || resolved.Status != "approved" {
		t.Fatal("existing resource did not resume", resolved, e)
	}
	p = permissionProxy(t, file)
	req, e = p.RequestAccess(context.Background(), "donald", "https://new.example.org/", "New website")
	if e != nil || req.Status != "approved" {
		t.Fatal(req, e)
	}
	if p.Permissions()["marv"].AllowAll {
		t.Fatal("allow all leaked to other actor")
	}
	info, e := os.Stat(file)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("permissions not owner-only", e)
	}
	if e = p.SetPermissions("donald", nil, nil, false); e != nil {
		t.Fatal(e)
	}
	p = permissionProxy(t, file)
	req, _ = p.RequestAccess(context.Background(), "donald", "https://example.com/", "After revocation")
	if req.Status != "pending" {
		t.Fatal("revocation did not persist")
	}
}
func TestAllowAllCannotBypassPrivateIPPortsOrLimits(t *testing.T) {
	p := permissionProxy(t, filepath.Join(t.TempDir(), "permissions.json"))
	if e := p.SetPermissions("donald", nil, nil, true); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{"http://127.0.0.1/", "http://8.8.8.8/", "http://a.local/", "http://host.internal/", "http://example.com:8080/"} {
		w := httptest.NewRecorder()
		p.Handler("donald").ServeHTTP(w, httptest.NewRequest("GET", raw, nil))
		if w.Code != 403 {
			t.Fatal("bypass", raw, w.Code)
		}
	}
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("169.254.169.254")}}, nil
	}
	if _, e := p.RequestAccess(context.Background(), "donald", "https://example.com/", "Private resolution"); e == nil {
		t.Fatal("private resolution approved")
	}
	for i := 0; i < 4; i++ {
		if !p.admit("donald", "example.com") {
			t.Fatal("public traffic unexpectedly denied")
		}
	}
	if p.admit("donald", "example.com") {
		t.Fatal("connection limit bypassed")
	}
}
func TestSessionNotRememberedAndSaveFailureGrantsNothing(t *testing.T) {
	file := filepath.Join(t.TempDir(), "permissions.json")
	p := permissionProxy(t, file)
	r, _ := p.RequestAccess(context.Background(), "donald", "https://example.com/", "Read")
	if _, e := p.ResolveAccessChoice(r.ID, "session"); e != nil {
		t.Fatal(e)
	}
	p = permissionProxy(t, file)
	r, _ = p.RequestAccess(context.Background(), "donald", "https://example.com/", "Read")
	if r.Status != "pending" {
		t.Fatal("session persisted")
	}
	p.permissionFile = filepath.Join(file, "unwritable.json")
	os.WriteFile(file, []byte("{}"), 0600)
	if _, e := p.ResolveAccessChoice(r.ID, "all"); e == nil {
		t.Fatal("save failure ignored")
	}
	if p.Permissions()["donald"].AllowAll {
		t.Fatal("failed save granted access")
	}
	if _, e := p.ResolveAccessChoice(r.ID, "anything"); e == nil {
		t.Fatal("unknown choice accepted")
	}
}

func TestRememberedChoicesReachFetchAndSandboxProxy(t *testing.T) {
	for _, choice := range []string{"always", "all"} {
		t.Run(choice, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.Write([]byte("Granted through the gateway"))
			}))
			defer srv.Close()
			p := permissionProxy(t, filepath.Join(t.TempDir(), "permissions.json"))
			r, _ := p.RequestAccess(context.Background(), "donald", "http://example.com/report", "Read report")
			if _, e := p.ResolveAccessChoice(r.ID, choice); e != nil {
				t.Fatal(e)
			}
			p.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
			}
			host := "example.com"
			if choice == "all" {
				host = "cdn.example.org"
			}
			if f, e := p.Fetch(context.Background(), "donald", "http://"+host+"/report", 1000); e != nil || f.Status != 200 {
				t.Fatal(f, e)
			}
			w := httptest.NewRecorder()
			p.BrowserHandler("donald").ServeHTTP(w, httptest.NewRequest("GET", "http://"+host+"/report", nil))
			if w.Code != 200 {
				t.Fatal(w.Code)
			}
			ctx, finish := p.requestContext(context.Background(), "donald", time.Minute)
			defer finish()
			if e := p.SetPermissions("donald", nil, nil, false); e != nil {
				t.Fatal(e)
			}
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("revocation did not cancel active context")
			}
			if _, e := p.Fetch(context.Background(), "donald", "http://"+host+"/report", 1000); e == nil {
				t.Fatal("fetch succeeded after revocation")
			}
		})
	}
}
func TestDamagedPermissionFileFailsClosed(t *testing.T) {
	for _, raw := range []string{`null`, `{"bad actor":{"allowAll":true}}`, `{"donald":{"domains":["*.example.com"],"allowAll":true}}`} {
		file := filepath.Join(t.TempDir(), "permissions.json")
		os.WriteFile(file, []byte(raw), 0600)
		p := New()
		if p.ConfigurePermissions(file) == nil {
			t.Fatal("accepted invalid saved permissions", raw)
		}
		if p.admit("donald", "example.com") {
			t.Fatal("bad config granted access")
		}
	}
}
