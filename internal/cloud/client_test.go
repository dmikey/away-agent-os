package cloud

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

type testVault struct {
	mu  sync.Mutex
	raw string
}

func (v *testVault) Save(_ string, value string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.raw = value
	return nil
}
func (v *testVault) Load(string) (string, error) { v.mu.Lock(); defer v.mu.Unlock(); return v.raw, nil }
func TestNativeBrowserPKCEKeychainRefreshAndLogout(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var issuer, nonce, challenge, redirect string
	var stateMu sync.Mutex
	var badNonce bool
	var refreshes int
	sign := func(exp time.Time) string {
		stateMu.Lock()
		n := nonce
		if badNonce {
			n = "wrong-nonce"
		}
		stateMu.Unlock()
		claims, _ := json.Marshal(map[string]any{"iss": issuer, "aud": "away-native", "sub": "alice", "nonce": n, "exp": exp.Unix()})
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test"}`))
		payload := header + "." + base64.RawURLEncoding.EncodeToString(claims)
		hash := sha256.Sum256([]byte(payload))
		signature, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
		if e != nil {
			panic(e)
		}
		return payload + "." + base64.RawURLEncoding.EncodeToString(signature)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/config":
			respond(w, 200, Config{Issuer: issuer, ClientID: "away-native"})
		case "/.well-known/openid-configuration":
			respond(w, 200, map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			respond(w, 200, map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "test", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
		case "/token":
			r.ParseForm()
			if r.Form.Get("grant_type") == "authorization_code" {
				stateMu.Lock()
				wantChallenge, wantRedirect := challenge, redirect
				stateMu.Unlock()
				sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
				if r.Form.Get("code") != "one-time-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != wantChallenge || r.Form.Get("redirect_uri") != wantRedirect {
					http.Error(w, "PKCE mismatch", 400)
					return
				}
			} else if r.Form.Get("grant_type") == "refresh_token" && r.Form.Get("refresh_token") == "refresh-secret-canary" {
				stateMu.Lock()
				refreshes++
				stateMu.Unlock()
			} else {
				http.Error(w, "invalid grant", 400)
				return
			}
			respond(w, 200, map[string]any{"token_type": "Bearer", "expires_in": 3600, "access_token": "access-secret-canary", "refresh_token": "refresh-secret-canary", "id_token": sign(time.Now().Add(time.Hour))})
		case "/v1/workspaces":
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer eyJ") {
				w.WriteHeader(401)
				return
			}
			respond(w, 200, []Workspace{})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	issuer = server.URL
	ctx, cancel := context.WithTimeout(oidc.ClientContext(context.Background(), server.Client()), 10*time.Second)
	defer cancel()
	vault := &testVault{}
	client := &Client{Vault: vault}
	client.OpenBrowser = func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil {
			return err
		}
		q := u.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("state") == "" || q.Get("nonce") == "" {
			return fmt.Errorf("missing native auth protections")
		}
		stateMu.Lock()
		nonce, challenge, redirect = q.Get("nonce"), q.Get("code_challenge"), q.Get("redirect_uri")
		stateMu.Unlock()
		// An unrelated page cannot complete the loopback login with a forged state.
		invalid, err := http.Get(q.Get("redirect_uri") + "?state=wrong&code=one-time-code")
		if err != nil {
			return err
		}
		invalid.Body.Close()
		if invalid.StatusCode != 400 {
			return fmt.Errorf("forged callback state accepted")
		}
		res, err := http.Get(q.Get("redirect_uri") + "?" + url.Values{"state": {q.Get("state")}, "code": {"one-time-code"}}.Encode())
		if err != nil {
			return err
		}
		defer res.Body.Close()
		io.Copy(io.Discard, res.Body)
		if res.StatusCode != 200 {
			return fmt.Errorf("callback failed")
		}
		return nil
	}
	status, err := client.SignIn(ctx, issuer)
	if err != nil || !status.Connected {
		t.Fatal("sign in failed", err)
	}
	raw, _ := json.Marshal(status)
	if strings.Contains(string(raw), "secret-canary") || strings.Contains(string(raw), "idToken") {
		t.Fatal("native UI received credentials")
	}
	saved, err := client.load()
	if err != nil || saved.Token.RefreshToken != "refresh-secret-canary" {
		t.Fatal("refresh token not vaulted", err)
	}
	// Expiry is checked on the identity token, independently of access-token expiry.
	saved.IDToken = sign(time.Now().Add(-time.Hour))
	if err = client.save(saved); err != nil {
		t.Fatal(err)
	}
	if _, err = client.Status(ctx); err != nil {
		t.Fatal("refresh failed", err)
	}
	stateMu.Lock()
	gotRefreshes := refreshes
	stateMu.Unlock()
	if gotRefreshes != 1 {
		t.Fatal("refresh was not performed")
	}
	if err = client.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if raw, _ := vault.Load(""); raw != "" {
		t.Fatal("logout retained session")
	}
	stateMu.Lock()
	badNonce = true
	stateMu.Unlock()
	if _, err = client.SignIn(ctx, issuer); err == nil {
		t.Fatal("mismatched nonce accepted")
	}
	if raw, _ := vault.Load(""); raw != "" {
		t.Fatal("invalid identity was vaulted")
	}
}
func TestNativeRejectsInsecureCloudAddresses(t *testing.T) {
	for _, raw := range []string{"http://cloud.example", "https://user:pass@cloud.example", "https://cloud.example?token=secret", "https://cloud.example#fragment"} {
		if err := HTTPSOrigin(raw, false); err == nil {
			t.Fatal("unsafe address accepted", raw)
		}
	}
}
