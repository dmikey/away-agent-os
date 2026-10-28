// Package gateway is the only network exit offered to a guest. It never trusts guest DNS or host paths.
package gateway

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const bodyLimit int64 = 4 * 1024 * 1024

var domainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

type Event struct {
	At       int64  `json:"at"`
	Actor    string `json:"actor"`
	Host     string `json:"host"`
	Method   string `json:"method"`
	Decision string `json:"decision"`
	Status   int    `json:"status"`
	Bytes    int64  `json:"bytes"`
}
type Proxy struct {
	access   map[string]*accessPending
	contexts map[string]map[string]context.CancelFunc
	mu       sync.Mutex
	policies map[string][]string
	events   []Event
	rates    map[string][]time.Time
	active   map[string]int
	lookup   func(context.Context, string) ([]net.IPAddr, error)
	dial     func(context.Context, string, string) (net.Conn, error)
}

func New() *Proxy {
	return &Proxy{contexts: map[string]map[string]context.CancelFunc{}, policies: map[string][]string{}, rates: map[string][]time.Time{}, active: map[string]int{}, lookup: net.DefaultResolver.LookupIPAddr, dial: (&net.Dialer{Timeout: 10 * time.Second}).DialContext}
}
func (p *Proxy) SetPolicy(actor string, domains []string) error {
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(actor) || len(domains) > 40 {
		return errors.New("Invalid network policy")
	}
	clean := []string{}
	seen := map[string]bool{}
	for _, d := range domains {
		d = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d)), ".")
		if !domainPattern.MatchString(d) || net.ParseIP(d) != nil || d == "localhost" || strings.HasSuffix(d, ".local") || strings.HasSuffix(d, ".internal") {
			return errors.New("Use exact public domain names; IP addresses, wildcards and internal domains are not allowed")
		}
		if !seen[d] {
			clean = append(clean, d)
			seen[d] = true
		}
	}
	p.mu.Lock()
	p.policies[actor] = clean
	cancels := []context.CancelFunc{}
	for _, cancel := range p.contexts[actor] {
		cancels = append(cancels, cancel)
	}
	p.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	return nil
}
func (p *Proxy) Policies() map[string][]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string][]string{}
	for actor, hosts := range p.policies {
		out[actor] = append([]string{}, hosts...)
	}
	return out
}
func (p *Proxy) Events() []Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Event{}, p.events...)
}
func (p *Proxy) record(e Event) {
	e.At = time.Now().UnixMilli()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append([]Event{e}, p.events...)
	if len(p.events) > 200 {
		p.events = p.events[:200]
	}
}
func (p *Proxy) admit(actor, host string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	allowed := false
	for _, d := range p.policies[actor] {
		if host == d {
			allowed = true
		}
	}
	if !allowed || p.active[actor] >= 4 {
		return false
	}
	now := time.Now()
	recent := p.rates[actor][:0]
	for _, t := range p.rates[actor] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	p.rates[actor] = recent
	if len(recent) >= 60 {
		return false
	}
	p.rates[actor] = append(recent, now)
	p.active[actor]++
	return true
}
func (p *Proxy) release(actor string) { p.mu.Lock(); defer p.mu.Unlock(); p.active[actor]-- }
func forbidden(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96"} {
		if netip.MustParsePrefix(cidr).Contains(ip) {
			return true
		}
	}
	// Also deny the Mac's own interface addresses, even if publicly routed.
	addresses, _ := net.InterfaceAddrs()
	for _, a := range addresses {
		if prefix, e := netip.ParsePrefix(a.String()); e == nil && prefix.Addr().Unmap() == ip {
			return true
		}
	}
	return false
}
func (p *Proxy) target(ctx context.Context, host, port string) (string, error) {
	ips, e := p.lookup(ctx, host)
	if e != nil || len(ips) == 0 {
		return "", errors.New("DNS lookup failed")
	}
	for _, entry := range ips {
		ip, ok := netip.AddrFromSlice(entry.IP)
		if !ok || forbidden(ip) {
			return "", errors.New("Private, local, host and metadata destinations are blocked")
		}
	}
	return net.JoinHostPort(ips[0].IP.String(), port), nil
}
func (p *Proxy) Handler(actor string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, port := r.URL.Hostname(), r.URL.Port()
		if r.Method == http.MethodConnect {
			var e error
			host, port, e = net.SplitHostPort(r.Host)
			if e != nil {
				http.Error(w, "Use host:443", 400)
				return
			}
		}
		host = strings.TrimSuffix(strings.ToLower(host), ".")
		if port == "" {
			port = "80"
		}
		event := Event{Actor: actor, Host: host, Method: r.Method, Decision: "blocked", Status: 403}
		defer func() { p.record(event) }()
		if !domainPattern.MatchString(host) || (r.Method == http.MethodConnect && port != "443") || (r.Method != http.MethodConnect && (port != "80" || r.URL.Scheme != "http" || r.URL.User != nil)) || !p.admit(actor, host) {
			http.Error(w, "Destination is not allowed by this actor's network policy", 403)
			return
		}
		defer p.release(actor)
		ctx, cancel := p.requestContext(r.Context(), actor, 60*time.Second)
		defer cancel()
		target, e := p.target(ctx, host, port)
		if e != nil {
			http.Error(w, e.Error(), 403)
			return
		}
		if r.Method == http.MethodConnect {
			upstream, e := p.dial(ctx, "tcp", target)
			if e != nil {
				event.Status = 502
				http.Error(w, "Upstream unavailable", 502)
				return
			}
			defer upstream.Close()
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "Tunnel unsupported", 500)
				return
			}
			client, buffer, e := hijacker.Hijack()
			if e != nil {
				return
			}
			defer client.Close()
			client.SetDeadline(time.Now().Add(60 * time.Second))
			upstream.SetDeadline(time.Now().Add(60 * time.Second))
			buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
			buffer.Flush()
			event.Decision = "allowed-tls-tunnel"
			event.Status = 200
			results := make(chan int64, 2)
			go func() { n, _ := io.Copy(upstream, io.LimitReader(buffer, bodyLimit)); results <- n }()
			go func() { n, _ := io.Copy(client, io.LimitReader(upstream, bodyLimit)); results <- n }()
			select {
			case n := <-results:
				event.Bytes += n
			case <-ctx.Done():
			}
			client.Close()
			upstream.Close()
			select {
			case n := <-results:
				event.Bytes += n
			case <-time.After(time.Second):
			}
			return
		}
		if r.ContentLength > 1024*1024 {
			event.Status = 413
			http.Error(w, "Request exceeds gateway limit", 413)
			return
		}
		request := r.Clone(ctx)
		request.RequestURI = ""
		request.URL.Host = net.JoinHostPort(host, port)
		request.Host = host
		request.Header = r.Header.Clone()
		stripHeaders(request.Header)
		request.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
		transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) { return p.dial(ctx, "tcp", target) }, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ResponseHeaderTimeout: 15 * time.Second, MaxResponseHeaderBytes: 65536, DisableKeepAlives: true}
		defer transport.CloseIdleConnections()
		response, e := transport.RoundTrip(request)
		if e != nil {
			event.Status = 502
			http.Error(w, "Gateway upstream request failed", 502)
			return
		}
		defer response.Body.Close()
		body, e := io.ReadAll(io.LimitReader(response.Body, bodyLimit+1))
		if e != nil || int64(len(body)) > bodyLimit {
			event.Status = 413
			http.Error(w, "Response exceeds gateway limit", 413)
			return
		}
		stripHeaders(response.Header)
		for key, values := range response.Header {
			for _, v := range values {
				w.Header().Add(key, v)
			}
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(response.StatusCode)
		w.Write(body)
		event.Decision = "allowed-http"
		event.Status = response.StatusCode
		event.Bytes = int64(len(body))
	})
}
func stripHeaders(h http.Header) {
	for _, connection := range h.Values("Connection") {
		for _, key := range strings.Split(connection, ",") {
			h.Del(strings.TrimSpace(key))
		}
	}
	for _, key := range []string{"Connection", "Proxy-Connection", "Proxy-Authorization", "Proxy-Authenticate", "Keep-Alive", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		h.Del(key)
	}
}

func (p *Proxy) requestContext(parent context.Context, actor string, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	idBytes := make([]byte, 16)
	rand.Read(idBytes)
	id := hex.EncodeToString(idBytes)
	p.mu.Lock()
	if p.contexts[actor] == nil {
		p.contexts[actor] = map[string]context.CancelFunc{}
	}
	p.contexts[actor][id] = cancel
	p.mu.Unlock()
	return ctx, func() { cancel(); p.mu.Lock(); delete(p.contexts[actor], id); p.mu.Unlock() }
}
