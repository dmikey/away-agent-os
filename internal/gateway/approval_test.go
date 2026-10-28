package gateway

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func approvalProxy() *Proxy {
	p := New()
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.215.14")}}, nil
	}
	return p
}
func TestAccessWaitsForHumanAndScopesDomain(t *testing.T) {
	p := approvalProxy()
	r, e := p.RequestAccess(context.Background(), "researcher", "https://example.com/report?secret=private#part", "Find primary evidence")
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(r.URL, "private") || strings.Contains(r.URL, "#") {
		t.Fatal("query/fragment leaked")
	}
	if len(p.Policies()["researcher"]) != 0 {
		t.Fatal("request granted itself")
	}
	ch := make(chan AccessRequest, 1)
	go func() { a, _ := p.WaitAccess(context.Background(), r.ID); ch <- a }()
	select {
	case <-ch:
		t.Fatal("did not wait")
	case <-time.After(10 * time.Millisecond):
	}
	again, _ := p.RequestAccess(context.Background(), "researcher", "https://example.com/another", "Evidence")
	if again.ID != r.ID {
		t.Fatal("duplicate prompt")
	}
	p.SetPolicy("researcher", []string{"docs.python.org"})
	if _, e = p.ResolveAccess(r.ID, true); e != nil {
		t.Fatal(e)
	}
	select {
	case a := <-ch:
		if a.Status != "approved" {
			t.Fatal(a)
		}
	case <-time.After(time.Second):
		t.Fatal("approval did not resume")
	}
	policy := p.Policies()
	if len(policy["researcher"]) != 2 || len(policy["cto"]) != 0 {
		t.Fatal(policy)
	}
	if _, e = p.ResolveAccess(r.ID, false); e == nil {
		t.Fatal("decision replay accepted")
	}
	if p.admit("cto", "example.com") || p.admit("researcher", "other.example.com") {
		t.Fatal("scope escaped")
	}
	already, e := p.RequestAccess(context.Background(), "researcher", "https://example.com/", "More evidence")
	if e != nil || already.Status != "approved" || len(p.AccessRequests()) != 0 {
		t.Fatal(already, e)
	}
	p.SetPolicy("researcher", []string{"docs.python.org"})
	revoked, _ := p.RequestAccess(context.Background(), "researcher", "https://example.com/", "Try again")
	if revoked.Status != "pending" {
		t.Fatal("revocation bypassed")
	}
}
func TestAccessDenialCancellationAndExpiry(t *testing.T) {
	p := approvalProxy()
	r, _ := p.RequestAccess(context.Background(), "seo", "https://example.com/", "Audit site")
	p.ResolveAccess(r.ID, false)
	a, e := p.WaitAccess(context.Background(), r.ID)
	if e != nil || a.Status != "denied" || len(p.Policies()["seo"]) != 0 {
		t.Fatal(a, e)
	}
	again, _ := p.RequestAccess(context.Background(), "seo", "https://example.com/again", "Try again")
	if again.Status != "denied" {
		t.Fatal("denial caused prompt spam")
	}
	c, _ := p.RequestAccess(context.Background(), "cto", "https://example.com/", "Docs")
	p.CancelAccess("seo", c.ID)
	if len(p.AccessRequests()) != 1 {
		t.Fatal("another actor cancelled request")
	}
	p.CancelAccess("cto", c.ID)
	a, _ = p.WaitAccess(context.Background(), c.ID)
	if a.Status != "cancelled" {
		t.Fatal(a)
	}
	if _, e = p.ResolveAccess(c.ID, true); e == nil {
		t.Fatal("cancelled request approved")
	}
	x, _ := p.RequestAccess(context.Background(), "ciso", "https://example.com/", "Research")
	p.mu.Lock()
	p.access[x.ID].request.Expires = time.Now().Add(-time.Second).UnixMilli()
	p.mu.Unlock()
	a, _ = p.WaitAccess(context.Background(), x.ID)
	if a.Status != "expired" {
		t.Fatal(a)
	}
	if _, e = p.ResolveAccess(x.ID, true); e == nil {
		t.Fatal("expired request approved")
	}
}
func TestAccessRejectsUnsafeTargets(t *testing.T) {
	p := approvalProxy()
	for _, u := range []string{"file:///etc/passwd", "https://127.0.0.1/", "https://localhost/", "https://x.local/", "https://x.internal/", "https://user:pass@example.com/", "https://example.com:8443/", "https://*.example.com/"} {
		if _, e := p.RequestAccess(context.Background(), "cto", u, "Test"); e == nil {
			t.Fatal("accepted", u)
		}
	}
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("169.254.169.254")}}, nil
	}
	if _, e := p.RequestAccess(context.Background(), "cto", "https://example.com/", "Test"); e == nil {
		t.Fatal("metadata destination accepted")
	}
	if len(p.AccessRequests()) != 0 {
		t.Fatal("unsafe request queued")
	}
}
func TestConcurrentAccessApprovalsPreserveBothDomains(t *testing.T) {
	p := approvalProxy()
	a, _ := p.RequestAccess(context.Background(), "cto", "https://one.example.com/", "One")
	b, _ := p.RequestAccess(context.Background(), "cto", "https://two.example.com/", "Two")
	done := make(chan struct{}, 2)
	for _, id := range []string{a.ID, b.ID} {
		go func(id string) { p.ResolveAccess(id, true); done <- struct{}{} }(id)
	}
	<-done
	<-done
	if len(p.Policies()["cto"]) != 2 {
		t.Fatal(p.Policies())
	}
}

func TestHumanApprovalEnablesSameSandboxGateway(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Approved primary source"))
	}))
	defer srv.Close()
	p := approvalProxy()
	p.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "93.184.215.14:80" {
			t.Error("unexpected target", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	if _, e := p.Fetch(context.Background(), "researcher", "http://example.com/report", 1000); e == nil || calls != 0 {
		t.Fatal("fetched before human approval")
	}
	r, e := p.RequestAccess(context.Background(), "researcher", "http://example.com/report", "Read primary source")
	if e != nil {
		t.Fatal(e)
	}
	p.ResolveAccess(r.ID, true)
	fetched, e := p.Fetch(context.Background(), "researcher", "http://example.com/report", 1000)
	if e != nil || !strings.Contains(fetched.Text, "Approved primary source") {
		t.Fatal(fetched, e)
	}
	w := httptest.NewRecorder()
	p.Handler("researcher").ServeHTTP(w, httptest.NewRequest("GET", "http://example.com/report", nil))
	if w.Code != 200 || calls != 2 {
		t.Fatal("guest proxy did not receive approved policy", w.Code, calls)
	}
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}}, nil
	}
	if _, e = p.Fetch(context.Background(), "researcher", "http://example.com/report", 1000); e == nil || calls != 2 {
		t.Fatal("DNS rebinding bypassed boundary")
	}
}
