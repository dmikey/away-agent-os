package gateway

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestDenyByDefaultAndPrivateResolution(t *testing.T) {
	p := New()
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	w := httptest.NewRecorder()
	p.Handler("milo").ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	if e := p.SetPolicy("milo", []string{"example.com"}); e != nil {
		t.Fatal(e)
	}
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("169.254.169.254")}}, nil
	}
	w = httptest.NewRecorder()
	p.Handler("milo").ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("metadata allowed")
	}
	w = httptest.NewRecorder()
	p.Handler("fern").ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-actor policy leaked")
	}
	for _, e := range p.Events() {
		if strings.Contains(e.Host, "?") {
			t.Fatal("query recorded")
		}
	}
}
func TestPolicyRejectsBypasses(t *testing.T) {
	p := New()
	for _, d := range []string{"127.0.0.1", "*.example.com", "localhost", "host.lima.internal", "a.local", "https://example.com", "example.com:8080"} {
		if p.SetPolicy("milo", []string{d}) == nil {
			t.Fatal("accepted", d)
		}
	}
	for _, s := range []string{"127.0.0.1", "::1", "169.254.169.254", "192.168.1.1", "10.0.0.1", "100.64.1.2", "::ffff:127.0.0.1"} {
		ip := net.ParseIP(s)
		v, _ := netip.AddrFromSlice(ip)
		if !forbidden(v) {
			t.Fatal("allowed private", s)
		}
	}
}

func TestPolicyChangeCancelsActiveActorRequests(t *testing.T) {
	p := New()
	p.SetPolicy("milo", []string{"example.com"})
	ctx, finish := p.requestContext(context.Background(), "milo", time.Minute)
	defer finish()
	p.SetPolicy("milo", nil)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("policy revocation did not cancel active traffic")
	}
}
func TestTextFetchTrimsHTMLAndOmitsQueryFromLog(t *testing.T) {
	p := New()
	p.SetPolicy("milo", []string{"example.com"})
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}
	p.dial = func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			http.ReadRequest(bufio.NewReader(server))
			body := "<html><script>SECRET_SCRIPT</script><style>HIDDEN_STYLE</style><p>Hello collaborative world</p><a href=\"/source\">Primary source</a><a href=\"javascript:alert(1)\">Bad</a></html>"
			fmt.Fprintf(server, "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
		}()
		return client, nil
	}
	result, e := p.Fetch(context.Background(), "milo", "http://example.com/?token=NOT_FOR_LOGS", 10)
	if e != nil {
		t.Fatal(e)
	}
	if len(result.Links) != 1 || result.Links[0] != "http://example.com/source" {
		t.Fatal("source links unavailable", result.Links)
	}
	if result.Text != "Hello coll" || !result.Truncated {
		t.Fatal(result)
	}
	events := p.Events()
	if len(events) != 1 || events[0].Host != "example.com" {
		t.Fatal(events)
	}
}
