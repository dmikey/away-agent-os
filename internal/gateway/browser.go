package gateway

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// BrowserHandler is the only uplink of the dedicated browser VM. Embedded resources
// and redirects need the same human decision as the top-level document.
func (p *Proxy) BrowserHandler(actor string) http.Handler {
	next := p.Handler(actor)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.String()
		if r.Method == http.MethodConnect {
			host := r.Host
			if !strings.HasSuffix(host, ":443") {
				http.Error(w, "Only HTTPS port 443 is supported", 403)
				return
			}
			raw = "https://" + host + "/"
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		req, e := p.RequestAccess(ctx, actor, raw, "The sandboxed website needs this domain to load a page or embedded resource.")
		if e != nil {
			http.Error(w, "Blocked by Away network policy", 403)
			return
		}
		if req.Status == "pending" {
			req, e = p.WaitAccess(ctx, req.ID)
		}
		if e != nil || req.Status != "approved" {
			http.Error(w, "Domain access was not approved", 403)
			return
		}
		next.ServeHTTP(w, r)
	})
}
