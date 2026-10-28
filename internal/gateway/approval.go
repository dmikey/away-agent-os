package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// AccessRequest is a human decision, not a policy writable by agent tools.
type AccessRequest struct {
	ID      string `json:"id"`
	Actor   string `json:"actor"`
	Host    string `json:"host"`
	URL     string `json:"url"`
	Reason  string `json:"reason"`
	Status  string `json:"status"`
	Created int64  `json:"created"`
	Expires int64  `json:"expires"`
}
type accessPending struct {
	request AccessRequest
	done    chan struct{}
}

func (p *Proxy) expireAccessLocked(a *accessPending) {
	if a.request.Status == "pending" && time.Now().UnixMilli() >= a.request.Expires {
		a.request.Status = "expired"
		close(a.done)
	}
}
func (p *Proxy) RequestAccess(ctx context.Context, actor, raw, reason string) (AccessRequest, error) {
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(actor) {
		return AccessRequest{}, errors.New("Invalid actor")
	}
	u, e := url.Parse(raw)
	if e != nil || len(raw) > 2000 || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return AccessRequest{}, errors.New("Use an HTTP(S) URL without credentials")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if net.ParseIP(host) != nil || !domainPattern.MatchString(host) || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return AccessRequest{}, errors.New("Use an exact public domain; local addresses, IPs and wildcards are blocked")
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	if !(u.Scheme == "https" && port == "443" || u.Scheme == "http" && port == "80") {
		return AccessRequest{}, errors.New("Only standard HTTP(S) ports are supported")
	}
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, e = p.target(check, host, port); e != nil {
		return AccessRequest{}, e
	}
	// Store only the reviewable origin/path. Queries and fragments may contain secrets.
	u.Host = host
	u.RawQuery = ""
	u.Fragment = ""
	u.ForceQuery = false
	reason = strings.TrimSpace(reason)
	if len(reason) > 500 {
		reason = reason[:500]
	}
	if reason == "" {
		return AccessRequest{}, errors.New("Explain why this domain is needed")
	}
	now := time.Now()
	var nonce [16]byte
	rand.Read(nonce[:])
	req := AccessRequest{ID: hex.EncodeToString(nonce[:]), Actor: actor, Host: host, URL: u.String(), Reason: reason, Status: "pending", Created: now.UnixMilli(), Expires: now.Add(3 * time.Minute).UnixMilli()}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.allowedLocked(actor, host) {
		req.Status = "approved"
		return req, nil
	}
	if p.access == nil {
		p.access = map[string]*accessPending{}
	}
	count := 0
	for id, a := range p.access {
		p.expireAccessLocked(a)
		if a.request.Expires < now.UnixMilli() && a.request.Status != "pending" {
			delete(p.access, id)
			continue
		}
		if a.request.Actor == actor && a.request.Host == host && (a.request.Status == "pending" || a.request.Status == "denied") {
			return a.request, nil
		}
		if a.request.Status == "pending" {
			count++
		}
	}
	if count >= 12 || len(p.access) >= 100 {
		return AccessRequest{}, errors.New("Finish existing access requests before asking for more")
	}
	p.access[req.ID] = &accessPending{request: req, done: make(chan struct{})}
	return req, nil
}
func (p *Proxy) AccessRequests() []AccessRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []AccessRequest{}
	for _, a := range p.access {
		p.expireAccessLocked(a)
		if a.request.Status == "pending" {
			out = append(out, a.request)
		}
	}
	return out
}
func (p *Proxy) ResolveAccess(id string, allow bool) (AccessRequest, error) {
	choice := "deny"
	if allow {
		choice = "session"
	}
	return p.ResolveAccessChoice(id, choice)
}
func (p *Proxy) ResolveAccessChoice(id, choice string) (AccessRequest, error) {
	if choice != "deny" && choice != "session" && choice != "always" && choice != "all" {
		return AccessRequest{}, errors.New("Unknown network permission choice")
	}
	allow := choice != "deny"
	p.mu.Lock()
	defer p.mu.Unlock()
	a := p.access[id]
	if a == nil {
		return AccessRequest{}, errors.New("Access request not found")
	}
	p.expireAccessLocked(a)
	if a.request.Status != "pending" {
		return a.request, errors.New("This request has already been resolved or expired")
	}
	if choice == "always" || choice == "all" {
		next := p.copyRememberedLocked()
		v := next[a.request.Actor]
		if choice == "all" {
			v.AllowAll = true
		} else {
			found := false
			for _, d := range v.Domains {
				if d == a.request.Host {
					found = true
				}
			}
			if !found {
				if len(v.Domains) >= 40 {
					return a.request, errors.New("This actor already has 40 remembered domains")
				}
				v.Domains = append(v.Domains, a.request.Host)
			}
		}
		next[a.request.Actor] = v
		if e := p.savePermissionsLocked(next); e != nil {
			return a.request, e
		}
		p.remembered = next
	}
	if choice == "session" {
		domains := p.policies[a.request.Actor]
		found := false
		for _, d := range domains {
			if d == a.request.Host {
				found = true
			}
		}
		if !found {
			if len(domains) >= 40 {
				return a.request, errors.New("This actor already has 40 approved domains")
			}
			p.policies[a.request.Actor] = append(domains, a.request.Host)
		}
	}
	if allow {
		a.request.Status = "approved"
	} else {
		a.request.Status = "denied"
	}
	decision := a.request.Status
	if choice == "always" {
		decision = "approved-always"
	}
	if choice == "all" {
		decision = "approved-all-public"
	}
	p.events = append([]Event{{At: time.Now().UnixMilli(), Actor: a.request.Actor, Host: a.request.Host, Method: "ACCESS", Decision: decision, Status: map[bool]int{true: 200, false: 403}[allow]}}, p.events...)
	if len(p.events) > 200 {
		p.events = p.events[:200]
	}
	close(a.done)
	p.approvePendingLocked(a.request.Actor)
	return a.request, nil
}
func (p *Proxy) CancelAccess(actor, id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	a := p.access[id]
	if a != nil && a.request.Actor == actor && a.request.Status == "pending" {
		a.request.Status = "cancelled"
		close(a.done)
	}
}
func (p *Proxy) WaitAccess(ctx context.Context, id string) (AccessRequest, error) {
	p.mu.Lock()
	a := p.access[id]
	if a == nil {
		p.mu.Unlock()
		return AccessRequest{}, errors.New("Access request not found")
	}
	p.expireAccessLocked(a)
	done := a.done
	deadline := a.request.Expires
	p.mu.Unlock()
	timer := time.NewTimer(time.Until(time.UnixMilli(deadline)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return AccessRequest{}, ctx.Err()
	case <-done:
	case <-timer.C:
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expireAccessLocked(a)
	return a.request, nil
}
