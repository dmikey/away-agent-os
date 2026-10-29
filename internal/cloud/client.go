package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// HTTPSOrigin accepts an HTTPS origin or issuer URL. Loopback HTTP is explicitly
// opt-in for operator development and is never accepted by the desktop client.
func HTTPSOrigin(raw string, loopback bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("Use a valid HTTPS address without credentials, query or fragment")
	}
	if u.Scheme == "https" {
		return nil
	}
	if loopback && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost") {
		return nil
	}
	return errors.New("Cloud connections require HTTPS")
}

type Vault interface {
	Save(string, string) error
	Load(string) (string, error)
}
type Client struct {
	mu          sync.Mutex
	Vault       Vault
	OpenBrowser func(string) error
}
type session struct {
	Endpoint string        `json:"endpoint"`
	Issuer   string        `json:"issuer"`
	ClientID string        `json:"clientId"`
	Subject  string        `json:"subject"`
	IDToken  string        `json:"idToken"`
	Token    *oauth2.Token `json:"token"`
}
type Connection struct {
	Connected  bool        `json:"connected"`
	Endpoint   string      `json:"endpoint"`
	Account    string      `json:"account"`
	Config     Config      `json:"config"`
	Workspaces []Workspace `json:"workspaces"`
}

const vaultName = "away-cloud-session-v1"

func boundedHTTP() *http.Client {
	return &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func clientJSON(ctx context.Context, method, raw, token string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, raw, body)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := httpFor(ctx).Do(req)
	if err != nil {
		return errors.New("Cloud service could not be reached")
	}
	defer res.Body.Close()
	if res.StatusCode == 401 {
		return errors.New("Your cloud sign-in expired or was revoked. Sign in again.")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var msg struct {
			Error string `json:"error"`
		}
		json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&msg)
		if msg.Error != "" {
			return errors.New(msg.Error)
		}
		return errors.New("Cloud request was declined")
	}
	return json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(out)
}
func discovery(ctx context.Context, endpoint string) (Config, error) {
	var config Config
	if err := HTTPSOrigin(endpoint, false); err != nil {
		return config, err
	}
	u, _ := url.Parse(endpoint)
	if u.Path != "" && u.Path != "/" {
		return config, errors.New("Use the cloud service origin, without a path")
	}
	if err := clientJSON(ctx, "GET", strings.TrimRight(endpoint, "/")+"/v1/config", "", nil, &config); err != nil {
		return config, err
	}
	if err := HTTPSOrigin(config.Issuer, false); err != nil {
		return config, err
	}
	if config.ClientID == "" {
		return config, errors.New("Cloud account sign-in is not configured")
	}
	return config, nil
}
func (c *Client) load() (session, error) {
	var s session
	raw, err := c.Vault.Load(vaultName)
	if err != nil {
		return s, err
	}
	if raw == "" {
		return s, nil
	}
	err = json.Unmarshal([]byte(raw), &s)
	return s, err
}
func (c *Client) save(s session) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return c.Vault.Save(vaultName, string(raw))
}
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Vault.Save(vaultName, "")
}
func (c *Client) SignIn(ctx context.Context, endpoint string) (Connection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	config, err := discovery(ctx, endpoint)
	if err != nil {
		return Connection{}, err
	}
	authCtx := oidc.ClientContext(ctx, httpFor(ctx))
	p, err := secureProvider(authCtx, config.Issuer)
	if err != nil {
		return Connection{}, errors.New("Account discovery failed")
	}
	// Native public client: no client secret, browser-based login, PKCE S256,
	// random state and nonce, and a short-lived loopback callback listener.
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return Connection{}, err
	}
	defer listener.Close()
	verifier := oauth2.GenerateVerifier()
	state, nonce := randomID(), randomID()
	redirect := "http://" + listener.Addr().String() + "/callback"
	oauth := oauth2.Config{ClientID: config.ClientID, Endpoint: p.Endpoint(), RedirectURL: redirect, Scopes: []string{oidc.ScopeOpenID, "profile", "offline_access"}}
	codes := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
		if r.Method != "GET" || r.URL.Query().Get("state") != state {
			http.Error(w, "Invalid sign-in response", 400)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Sign-in was declined. Return to Away.", 400)
			return
		}
		select {
		case codes <- code:
			io.WriteString(w, "You're signed in. You can return to Away OS.")
		default:
			http.Error(w, "This sign-in has already been used", 409)
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	go srv.Serve(listener)
	defer srv.Close()
	if c.OpenBrowser == nil {
		return Connection{}, errors.New("System browser unavailable")
	}
	if err = c.OpenBrowser(oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce))); err != nil {
		return Connection{}, err
	}
	var code string
	select {
	case code = <-codes:
	case <-ctx.Done():
		return Connection{}, errors.New("Sign-in cancelled or timed out")
	}
	token, err := oauth.Exchange(authCtx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Connection{}, errors.New("Sign-in exchange failed")
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return Connection{}, errors.New("Account did not return an identity token")
	}
	id, err := p.Verifier(&oidc.Config{ClientID: config.ClientID}).Verify(authCtx, raw)
	if err != nil || id.Nonce != nonce || id.Subject == "" {
		return Connection{}, errors.New("Account identity could not be verified")
	}
	s := session{endpoint, config.Issuer, config.ClientID, id.Subject, raw, token}
	// Prove this account is admitted BEFORE saving it as a working connection.
	rows := []Workspace{}
	if err = clientJSON(ctx, "GET", endpoint+"/v1/workspaces", raw, nil, &rows); err != nil {
		return Connection{}, err
	}
	if err = c.save(s); err != nil {
		return Connection{}, err
	}
	return Connection{true, endpoint, id.Subject, config, rows}, nil
}
func (c *Client) authorized(ctx context.Context) (session, error) {
	s, err := c.load()
	if err != nil {
		return s, err
	}
	if s.Endpoint == "" {
		return s, errors.New("Connect your Away Cloud account first")
	}
	if err = HTTPSOrigin(s.Endpoint, false); err != nil {
		return s, err
	}
	if err = HTTPSOrigin(s.Issuer, false); err != nil {
		return s, err
	}
	ctx = oidc.ClientContext(ctx, httpFor(ctx))
	p, err := secureProvider(ctx, s.Issuer)
	if err != nil {
		return s, errors.New("Account discovery failed")
	}
	verifier := p.Verifier(&oidc.Config{ClientID: s.ClientID})
	id, verifyErr := verifier.Verify(ctx, s.IDToken)
	if verifyErr == nil && id.Subject == s.Subject && time.Until(id.Expiry) > time.Minute {
		return s, nil
	}
	if s.Token == nil || s.Token.RefreshToken == "" {
		return s, errors.New("Sign in again to reconnect your cloud workspace")
	}
	expired := *s.Token
	expired.Expiry = time.Now().Add(-time.Hour)
	oauth := oauth2.Config{ClientID: s.ClientID, Endpoint: p.Endpoint()}
	token, err := oauth.TokenSource(ctx, &expired).Token()
	if err != nil {
		return s, errors.New("Cloud sign-in expired. Sign in again.")
	}
	raw, _ := token.Extra("id_token").(string)
	id, err = verifier.Verify(ctx, raw)
	if err != nil || id.Subject != s.Subject {
		return s, errors.New("Refreshed account identity could not be verified")
	}
	s.IDToken = raw
	s.Token = token
	return s, c.save(s)
}
func (c *Client) Status(ctx context.Context) (Connection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	saved, err := c.load()
	if err != nil {
		return Connection{}, err
	}
	if saved.Endpoint == "" {
		return Connection{Workspaces: []Workspace{}}, nil
	}
	s, err := c.authorized(ctx)
	if err != nil {
		return Connection{Endpoint: saved.Endpoint}, err
	}
	config, err := discovery(ctx, s.Endpoint)
	if err != nil {
		return Connection{}, err
	}
	var rows []Workspace
	if err = clientJSON(ctx, "GET", s.Endpoint+"/v1/workspaces", s.IDToken, nil, &rows); err != nil {
		return Connection{}, err
	}
	return Connection{true, s.Endpoint, s.Subject, config, rows}, nil
}
func (c *Client) Action(ctx context.Context, id, action, payload string) (Workspace, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out Workspace
	s, err := c.authorized(ctx)
	if err != nil {
		return out, err
	}
	path := "/v1/workspaces"
	if action != "plan" {
		if len(id) != 48 || strings.ContainsAny(id, "/?.") {
			return out, ErrNotFound
		}
		switch action {
		case "launch", "discard", "close", "refresh", "lease":
		default:
			return out, errors.New("Unsupported cloud action")
		}
		path += "/" + id + "/" + action
	}
	err = clientJSON(ctx, "POST", s.Endpoint+path, s.IDToken, strings.NewReader(payload), &out)
	return out, err
}

func (c *Client) Bids(ctx context.Context, id string) ([]Bid, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, err := c.authorized(ctx)
	if err != nil {
		return nil, err
	}
	if len(id) != 48 || strings.ContainsAny(id, "/?.") {
		return nil, ErrNotFound
	}
	var bids []Bid
	err = clientJSON(ctx, "GET", s.Endpoint+"/v1/workspaces/"+id+"/bids", s.IDToken, nil, &bids)
	return bids, err
}

func secureProvider(ctx context.Context, issuer string) (*oidc.Provider, error) {
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	var metadata struct {
		Keys string `json:"jwks_uri"`
	}
	if err = p.Claims(&metadata); err != nil {
		return nil, err
	}
	for _, raw := range []string{p.Endpoint().AuthURL, p.Endpoint().TokenURL, metadata.Keys} {
		if err = HTTPSOrigin(raw, false); err != nil {
			return nil, errors.New("Account endpoints must use HTTPS")
		}
	}
	return p, nil
}

func httpFor(ctx context.Context) *http.Client {
	if client, ok := ctx.Value(oauth2.HTTPClient).(*http.Client); ok && client != nil {
		return client
	}
	return boundedHTTP()
}

func (c *Client) Endpoint() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, err := c.load()
	if err != nil {
		return "", err
	}
	if s.Endpoint != "" {
		if err = HTTPSOrigin(s.Endpoint, false); err != nil {
			return "", err
		}
	}
	return s.Endpoint, nil
}
