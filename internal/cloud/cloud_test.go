package cloud

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/coreos/go-oidc/v3/oidc"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testImage = "ghcr.io/example/away-worker@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testProject = "12345678-1234-1234-1234-123456789abc"

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}
func fixture(t *testing.T) (*Server, *Store) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	srv := &Server{Store: store, Config: Config{WorkerImage: testImage}, Authenticate: func(_ context.Context, token string) (string, error) {
		if token == "alice" || token == "bob" {
			return token, nil
		}
		return "", errors.New("denied")
	}}
	return srv, store
}
func request(t *testing.T, s *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	r := httptest.NewRequest(method, path, &buf)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func plan(t *testing.T, s *Server) Workspace {
	t.Helper()
	w := request(t, s, "POST", "/v1/workspaces", "alice", map[string]any{"name": "Alice's studio", "runtimeHours": 8})
	if w.Code != 201 {
		t.Fatalf("plan: %d %s", w.Code, w.Body)
	}
	var row Workspace
	if err := json.Unmarshal(w.Body.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	return row
}
func TestTenantIsolationAndDefaultLaunchDenial(t *testing.T) {
	s, _ := fixture(t)
	row := plan(t, s)
	if w := request(t, s, "GET", "/v1/workspaces/"+row.ID, "bob", nil); w.Code != 404 {
		t.Fatal("cross-tenant record exposed", w.Code)
	}
	if w := request(t, s, "GET", "/v1/workspaces", "", nil); w.Code != 401 {
		t.Fatal("unauthenticated access", w.Code)
	}
	if w := request(t, s, "POST", "/v1/workspaces/"+row.ID+"/launch", "alice", map[string]any{"planHash": row.PlanHash}); w.Code != 403 {
		t.Fatal("default launch permitted", w.Code)
	}
	w := request(t, s, "GET", "/v1/workspaces", "alice", nil)
	if strings.Contains(w.Body.String(), `"owner"`) {
		t.Fatal("internal owner leaked")
	}
	r := httptest.NewRequest("GET", "/v1/config", nil)
	r.Header.Set("Origin", "https://evil.example")
	out := httptest.NewRecorder()
	s.Handler().ServeHTTP(out, r)
	if out.Code != 403 {
		t.Fatal("web origin accepted")
	}
	s.Config.ExecutionReady = true
	out = request(t, s, "GET", "/v1/config", "", nil)
	if !strings.Contains(out.Body.String(), `"executionReady":false`) {
		t.Fatal("unverified execution advertised")
	}
}
func TestLaunchReplayAndInterruptedRecovery(t *testing.T) {
	s, store := fixture(t)
	s.Config.LaunchEnabled = true
	row := plan(t, s)
	var calls atomic.Int32
	s.Akash = &Akash{Key: "operator-secret", Project: testProject, Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("ambiguous disconnect")
	})}}
	path := "/v1/workspaces/" + row.ID + "/launch"
	if w := request(t, s, "POST", path, "alice", map[string]any{"planHash": "stale"}); w.Code != 409 {
		t.Fatal("stale approval accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("called Akash before approval")
	}
	for range 2 {
		if w := request(t, s, "POST", path, "alice", map[string]any{"planHash": row.PlanHash}); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls.Load() != 1 {
		t.Fatal("replayed paid create", calls.Load())
	}
	got, _ := store.Get("alice", row.ID)
	if got.State != "reconcile" {
		t.Fatal(got.State)
	}
	if _, err := store.Plan("alice", "duplicate", testImage, 8); err == nil {
		t.Fatal("duplicate infrastructure during reconciliation")
	}
	pathDB := filepath.Join(t.TempDir(), "recovery.db")
	other, err := OpenStore(pathDB)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := other.Plan("alice", "recovery", testImage, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = other.Transition("alice", pending.ID, "review", "leasing", "test crash", "123"); err != nil {
		t.Fatal(err)
	}
	other.Close()
	other, err = OpenStore(pathDB)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err = other.Recover(); err != nil {
		t.Fatal(err)
	}
	got, _ = other.Get("alice", pending.ID)
	if got.State != "reconcile" || got.Dseq != "123" {
		t.Fatal("lost recovery metadata", got)
	}
}
func TestConcurrentLaunchAndObserverDisconnect(t *testing.T) {
	s, store := fixture(t)
	s.Config.LaunchEnabled = true
	row := plan(t, s)
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	s.Akash = &Akash{Key: "secret", Project: testProject, Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) != 1 {
			t.Error("duplicate create")
		}
		close(entered)
		<-release
		if r.Context().Err() != nil {
			t.Error("observer disconnect cancelled provisioning")
		}
		return response(201, `{"data":{"dseq":"123"}}`), nil
	})}}
	payload, _ := json.Marshal(map[string]any{"planHash": row.PlanHash})
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("POST", "/v1/workspaces/"+row.ID+"/launch", bytes.NewReader(payload)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer alice")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); s.Handler().ServeHTTP(httptest.NewRecorder(), r) }()
	<-entered
	cancel()
	for range 3 {
		request(t, s, "POST", "/v1/workspaces/"+row.ID+"/launch", "alice", map[string]any{"planHash": row.PlanHash})
	}
	close(release)
	wg.Wait()
	got, _ := store.Get("alice", row.ID)
	if calls.Load() != 1 || got.State != "bidding" || got.Dseq != "123" {
		t.Fatal("incorrect durable launch", calls.Load(), got)
	}
}
func TestAkashWireContractAndSecretRedaction(t *testing.T) {
	manifest, err := SDL(testImage)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(manifest, "tee: cpu") || strings.Contains(manifest, "secret") {
		t.Fatal("unsafe manifest")
	}
	if _, err = SDL("image:latest\nenv: [SECRET=bad]"); err == nil {
		t.Fatal("manifest injection accepted")
	}
	a := &Akash{Key: "secret-canary", Project: testProject, Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != AkashURL+"/v1/deployments" || r.Header.Get("x-api-key") != "secret-canary" || r.Header.Get("x-project-id") != testProject {
			t.Fatal("wrong API boundary")
		}
		var body struct {
			Data struct {
				SDL     string `json:"sdl"`
				Project string `json:"projectId"`
				Hours   int    `json:"runtimeLimitHours"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Data.Project != testProject || body.Data.Hours != 8 || body.Data.SDL != manifest || strings.Contains(body.Data.SDL, "secret-canary") {
			t.Fatal("invalid create body")
		}
		return response(201, `{"data":{"dseq":"456"}}`), nil
	})}}
	id, err := a.Create(context.Background(), manifest, 8)
	if err != nil || id != "456" {
		t.Fatal(id, err)
	}
	a.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) { return response(403, `{"message":"secret-canary"}`), nil })
	if err = a.Check(context.Background()); err == nil || strings.Contains(err.Error(), "secret-canary") {
		t.Fatal("secret reflected", err)
	}
}
func TestLeaseRequiresCurrentQuoteAndNeverUnlocksWorker(t *testing.T) {
	s, store := fixture(t)
	s.Config.LaunchEnabled = true
	row := plan(t, s)
	store.Transition("alice", row.ID, "review", "bidding", "fixture", "123")
	var leases atomic.Int32
	s.Akash = &Akash{Key: "secret", Project: testProject, Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" {
			return response(200, `{"data":[{"bid":{"id":{"dseq":"123","gseq":1,"oseq":1,"provider":"akash1test"},"state":"open","price":{"denom":"uact","amount":"100"}}}]}`), nil
		}
		leases.Add(1)
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), `"data"`) || !strings.Contains(string(raw), `"leases"`) {
			t.Fatal("wrong lease envelope", string(raw))
		}
		return response(200, `{}`), nil
	})}}
	path := "/v1/workspaces/" + row.ID + "/lease"
	if w := request(t, s, "POST", path, "alice", map[string]any{"planHash": row.PlanHash, "provider": "akash1test", "amount": "99"}); w.Code != 409 {
		t.Fatal("stale quote accepted")
	}
	if w := request(t, s, "POST", path, "alice", map[string]any{"planHash": row.PlanHash, "provider": "akash1test", "amount": "100"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	got, _ := store.Get("alice", row.ID)
	if got.State != "verification_required" || leases.Load() != 1 {
		t.Fatal("unverified worker unlocked", got.State)
	}
}
func TestOIDCClaimsAndAccountAllowlist(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var issuer string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			respond(w, 200, map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			respond(w, 200, map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "test", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	issuer = server.URL
	ctx := oidc.ClientContext(context.Background(), server.Client())
	auth, err := OIDCAuth(ctx, issuer, "away-native", []string{"alice"})
	if err != nil {
		t.Fatal(err)
	}
	sign := func(claims map[string]any) string {
		raw, _ := json.Marshal(claims)
		head := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test"}`))
		payload := head + "." + base64.RawURLEncoding.EncodeToString(raw)
		sum := sha256.Sum256([]byte(payload))
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
		if err != nil {
			t.Fatal(err)
		}
		return payload + "." + base64.RawURLEncoding.EncodeToString(signature)
	}
	for _, bad := range []string{"", "aud", "iss", "sub", "exp"} {
		claims := map[string]any{"iss": issuer, "aud": "away-native", "sub": "alice", "exp": time.Now().Add(time.Hour).Unix()}
		if bad == "exp" {
			claims[bad] = time.Now().Add(-time.Hour).Unix()
		} else if bad != "" {
			claims[bad] = "wrong"
		}
		owner, err := auth(ctx, sign(claims))
		if bad == "" {
			if err != nil || owner != issuer+"\n"+"alice" {
				t.Fatal("valid account denied", err)
			}
		} else if err == nil {
			t.Fatal("invalid claim accepted", bad)
		}
	}
}
