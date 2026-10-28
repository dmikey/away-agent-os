//go:build darwin && !browsercheck

package macvm

import "net/http"

func (m *Manager) browserProxyHandler(actor string) http.Handler {
	return m.Gateway.BrowserHandler(actor)
}

func browserKernelArgs() string { return "" }
