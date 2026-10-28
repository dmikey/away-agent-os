package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Permission struct {
	Domains    []string `json:"domains"`
	Remembered []string `json:"remembered"`
	AllowAll   bool     `json:"allowAll"`
}
type savedPermission struct {
	Domains  []string `json:"domains"`
	AllowAll bool     `json:"allowAll"`
}

func validActor(a string) bool { return regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(a) }
func publicDomain(d string) bool {
	return domainPattern.MatchString(d) && net.ParseIP(d) == nil && !strings.HasSuffix(d, ".local") && !strings.HasSuffix(d, ".internal")
}
func cleanDomains(ds []string) ([]string, error) {
	if len(ds) > 40 {
		return nil, errors.New("Approve up to 40 exact domains")
	}
	out := []string{}
	seen := map[string]bool{}
	for _, d := range ds {
		d = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d)), ".")
		if !publicDomain(d) {
			return nil, errors.New("Use exact public domains; IPs, wildcards and internal domains are blocked")
		}
		if !seen[d] {
			out = append(out, d)
			seen[d] = true
		}
	}
	return out, nil
}

// ConfigurePermissions loads human-owned preferences, outside the agent workspace.
func (p *Proxy) ConfigurePermissions(file string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	saved := map[string]savedPermission{}
	b, e := os.ReadFile(file)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if e == nil {
		if len(b) > 1024*1024 || json.Unmarshal(b, &saved) != nil || saved == nil || len(saved) > 256 {
			return errors.New("Invalid saved network permissions")
		}
	}
	for actor, v := range saved {
		if !validActor(actor) {
			return errors.New("Invalid saved network actor")
		}
		ds, e := cleanDomains(v.Domains)
		if e != nil {
			return e
		}
		v.Domains = ds
		saved[actor] = v
	}
	p.permissionFile = file
	p.remembered = saved
	return nil
}
func (p *Proxy) savePermissionsLocked(next map[string]savedPermission) error {
	if p.permissionFile == "" {
		return errors.New("Persistent network permissions are not configured")
	}
	if len(next) > 256 {
		return errors.New("Too many saved network actors")
	}
	b, e := json.MarshalIndent(next, "", "  ")
	if e != nil {
		return e
	}
	dir := filepath.Dir(p.permissionFile)
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".network-permissions-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), p.permissionFile)
}
func (p *Proxy) copyRememberedLocked() map[string]savedPermission {
	out := map[string]savedPermission{}
	for a, v := range p.remembered {
		v.Domains = append([]string{}, v.Domains...)
		out[a] = v
	}
	return out
}
func (p *Proxy) allowedLocked(actor, host string) bool {
	if !validActor(actor) || !publicDomain(host) {
		return false
	}
	v := p.remembered[actor]
	if v.AllowAll {
		return true
	}
	for _, d := range append(append([]string{}, p.policies[actor]...), v.Domains...) {
		if d == host {
			return true
		}
	}
	return false
}
func (p *Proxy) Permissions() map[string]Permission {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]Permission{}
	for a, ds := range p.policies {
		out[a] = Permission{Domains: append([]string{}, ds...), Remembered: []string{}}
	}
	for a, v := range p.remembered {
		x := out[a]
		if x.Domains == nil {
			x.Domains = []string{}
		}
		x.Remembered = append([]string{}, v.Domains...)
		x.AllowAll = v.AllowAll
		out[a] = x
	}
	return out
}

// SetPermissions is only exposed to the human settings UI, never an agent tool.
func (p *Proxy) SetPermissions(actor string, session, remembered []string, all bool) error {
	if !validActor(actor) {
		return errors.New("Invalid actor")
	}
	ds, e := cleanDomains(session)
	if e != nil {
		return e
	}
	rs, e := cleanDomains(remembered)
	if e != nil {
		return e
	}
	p.mu.Lock()
	next := p.copyRememberedLocked()
	if len(rs) == 0 && !all {
		delete(next, actor)
	} else {
		next[actor] = savedPermission{Domains: rs, AllowAll: all}
	}
	if e = p.savePermissionsLocked(next); e != nil {
		p.mu.Unlock()
		return e
	}
	p.remembered = next
	p.policies[actor] = ds
	cancels := []context.CancelFunc{}
	for _, cancel := range p.contexts[actor] {
		cancels = append(cancels, cancel)
	}
	p.approvePendingLocked(actor)
	p.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	return nil
}
func (p *Proxy) approvePendingLocked(actor string) {
	for _, a := range p.access {
		p.expireAccessLocked(a)
		if a.request.Status == "pending" && a.request.Actor == actor && p.allowedLocked(actor, a.request.Host) {
			a.request.Status = "approved"
			close(a.done)
		}
	}
}
