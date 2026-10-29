package main

import (
	"context"
	"errors"
	"time"

	"github.com/dmikey/away-agent-os/desktop/internal/cloud"
	"github.com/dmikey/away-agent-os/desktop/internal/credentials"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) cloudClient() *cloud.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cloud == nil {
		a.cloud = &cloud.Client{Vault: credentials.Keychain{}, OpenBrowser: func(raw string) error { wailsruntime.BrowserOpenURL(a.ctx, raw); return nil }}
	}
	return a.cloud
}
func (a *App) CloudStatus() (cloud.Connection, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 35*time.Second)
	defer cancel()
	return a.cloudClient().Status(ctx)
}
func (a *App) CloudSignIn(endpoint string) (cloud.Connection, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 3*time.Minute)
	defer cancel()
	a.mu.Lock()
	if old := a.requests["cloud-login"]; old != nil {
		a.mu.Unlock()
		return cloud.Connection{}, errors.New("A cloud sign-in is already open. Complete or cancel it first.")
	}
	a.requests["cloud-login"] = cancel
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.requests, "cloud-login"); a.mu.Unlock() }()
	return a.cloudClient().SignIn(ctx, endpoint)
}
func (a *App) CloudDisconnect() error { return a.cloudClient().Disconnect() }

// CloudAction only manages infrastructure. No cloud token or operator credential
// is exposed through the workspace MCP or an agent-callable action.
func (a *App) CloudAction(id, action, payload string) (cloud.Workspace, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
	defer cancel()
	return a.cloudClient().Action(ctx, id, action, payload)
}

func (a *App) CloudBids(id string) ([]cloud.Bid, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 35*time.Second)
	defer cancel()
	return a.cloudClient().Bids(ctx, id)
}

func (a *App) CloudEndpoint() (string, error) { return a.cloudClient().Endpoint() }
