package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

type Config struct {
	Issuer         string `json:"issuer"`
	ClientID       string `json:"clientId"`
	LaunchEnabled  bool   `json:"launchEnabled"`
	WorkerImage    string `json:"workerImage,omitempty"`
	ExecutionReady bool   `json:"executionReady"`
}
type Authenticator func(context.Context, string) (string, error)
type Server struct {
	Store        *Store
	Akash        *Akash
	Config       Config
	Authenticate Authenticator
}

// OIDCAuth validates signature, issuer, audience and expiry, then checks the
// operator's allowlist on EVERY request. Removing a subject revokes its access.
func OIDCAuth(ctx context.Context, issuer, clientID string, subjects []string) (Authenticator, error) {
	if err := HTTPSOrigin(issuer, false); err != nil {
		return nil, err
	}
	provider, err := secureProvider(ctx, issuer)
	if err != nil {
		return nil, errors.New("OIDC discovery failed")
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: clientID})
	allowed := map[string]bool{}
	for _, sub := range subjects {
		if s := strings.TrimSpace(sub); s != "" {
			allowed[s] = true
		}
	}
	if clientID == "" || len(allowed) == 0 {
		return nil, errors.New("Configure the OIDC client and allowed account subjects")
	}
	return func(ctx context.Context, token string) (string, error) {
		id, err := verifier.Verify(ctx, token)
		if err != nil || id.Subject == "" || !allowed[id.Subject] {
			return "", errors.New("Account is not authorized")
		}
		return issuer + "\n" + id.Subject, nil
	}, nil
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/config", func(w http.ResponseWriter, r *http.Request) {
		config := s.Config
		config.ExecutionReady = false
		respond(w, 200, config)
	})
	mux.HandleFunc("/v1/workspaces", s.protected(s.workspaces))
	mux.HandleFunc("/v1/workspaces/", s.protected(s.workspace))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		// There is deliberately no CORS allowance or cookie authentication.
		if r.Header.Get("Origin") != "" {
			respond(w, 403, map[string]string{"error": "Use the native Away connection"})
			return
		}
		mux.ServeHTTP(w, r)
	})
}

type protectedHandler func(http.ResponseWriter, *http.Request, string)

func (s *Server) protected(next protectedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || len(auth) > 16384 || s.Authenticate == nil {
			respond(w, 401, map[string]string{"error": "Sign in to Away Cloud"})
			return
		}
		owner, err := s.Authenticate(r.Context(), strings.TrimPrefix(auth, "Bearer "))
		if err != nil || owner == "" {
			respond(w, 401, map[string]string{"error": "Sign in to an authorized Away account"})
			return
		}
		next(w, r, owner)
	}
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		respond(w, 400, map[string]string{"error": "Invalid request"})
		return false
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		respond(w, 400, map[string]string{"error": "Invalid request"})
		return false
	}
	return true
}
func respond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
func failure(w http.ResponseWriter, err error) {
	status := 409
	if errors.Is(err, ErrNotFound) {
		status = 404
	}
	respond(w, status, map[string]string{"error": err.Error()})
}
func (s *Server) workspaces(w http.ResponseWriter, r *http.Request, owner string) {
	switch r.Method {
	case "GET":
		rows, err := s.Store.List(owner)
		if err != nil {
			respond(w, 500, map[string]string{"error": "Workspace storage unavailable"})
			return
		}
		respond(w, 200, rows)
	case "POST":
		var body struct {
			Name         string `json:"name"`
			RuntimeHours int    `json:"runtimeHours"`
		}
		if !decode(w, r, &body) {
			return
		}
		row, err := s.Store.Plan(owner, strings.TrimSpace(body.Name), s.Config.WorkerImage, body.RuntimeHours)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 201, row)
	default:
		w.WriteHeader(405)
	}
}
func (s *Server) workspace(w http.ResponseWriter, r *http.Request, owner string) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/workspaces/"), "/")
	if len(parts) > 2 || len(parts[0]) != 48 {
		w.WriteHeader(404)
		return
	}
	row, err := s.Store.Get(owner, parts[0])
	if err != nil {
		failure(w, err)
		return
	}
	if len(parts) == 1 && r.Method == "GET" {
		respond(w, 200, row)
		return
	}
	if len(parts) != 2 {
		w.WriteHeader(405)
		return
	}
	action := parts[1]
	if (action == "refresh" || action == "close") && (row.State == "creating" || row.State == "leasing" || row.State == "closing") {
		failure(w, ErrConflict)
		return
	}
	if action == "bids" && r.Method == "GET" && row.State == "bidding" {
		bids, err := s.Akash.Bids(r.Context(), row.Dseq)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, bids)
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var body struct {
		PlanHash string `json:"planHash"`
		Provider string `json:"provider"`
		Amount   string `json:"amount"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.PlanHash != row.PlanHash {
		failure(w, errors.New("Review the current plan before continuing"))
		return
	}
	if action == "discard" && row.State == "review" {
		row, err = s.Store.Transition(owner, row.ID, "review", "discarded", "Plan discarded; no infrastructure was created.", "")
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, row)
		return
	}
	if action == "launch" {
		if !s.Config.LaunchEnabled || s.Akash == nil {
			respond(w, 403, map[string]string{"error": "Infrastructure launches are disabled by the operator"})
			return
		}
		if row.State != "review" {
			respond(w, 200, row)
			return
		} // replay never repeats a create
		row, err = s.Store.Transition(owner, row.ID, "review", "creating", "Account approved this exact resource plan and runtime limit.", "")
		if err != nil {
			failure(w, err)
			return
		}
		// Owner disconnect does not cancel the infrastructure request. A bounded
		// service-owned operation persists its result before a later UI reconnect.
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
		defer cancel()
		dseq, callErr := s.Akash.Create(ctx, row.Manifest, row.RuntimeHours)
		if callErr != nil {
			row, err = s.Store.Transition(owner, row.ID, "creating", "reconcile", "Creation outcome is uncertain. Do not create another deployment; reconcile in Akash Console.", "")
		} else {
			row, err = s.Store.Transition(owner, row.ID, "creating", "bidding", "Deployment created. Choose a provider bid before leasing.", dseq)
		}
	} else if action == "lease" {
		if !s.Config.LaunchEnabled || s.Akash == nil || row.State != "bidding" {
			failure(w, ErrConflict)
			return
		}
		bids, bidErr := s.Akash.Bids(r.Context(), row.Dseq)
		if bidErr != nil {
			failure(w, bidErr)
			return
		}
		var selected *Bid
		for i := range bids {
			b := &bids[i]
			if b.ID.Provider == body.Provider && b.Price.Amount == body.Amount && b.Price.Denom == "uact" && b.ID.Gseq == 1 && b.ID.Oseq == 1 {
				selected = b
				break
			}
		}
		if selected != nil {
			price, ok := new(big.Rat).SetString(selected.Price.Amount)
			if !ok || price.Sign() <= 0 || price.Cmp(big.NewRat(1000, 1)) > 0 {
				selected = nil
			}
		}
		if selected == nil {
			failure(w, errors.New("Provider quote changed; review the current bids"))
			return
		}
		row, err = s.Store.Transition(owner, row.ID, "bidding", "leasing", "Account accepted provider "+selected.ID.Provider+" at "+selected.Price.Amount+" uact per block.", "")
		if err != nil {
			failure(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
		defer cancel()
		if callErr := s.Akash.Lease(ctx, *selected); callErr != nil {
			row, err = s.Store.Transition(owner, row.ID, "leasing", "reconcile", "Lease outcome is uncertain. Inspect the existing deployment before further changes.", "")
		} else {
			row, err = s.Store.Transition(owner, row.ID, "leasing", "verification_required", "Lease submitted. Execution, files and credentials remain locked until worker attestation is implemented and verified.", "")
		}
	} else if action == "refresh" {
		if row.Dseq == "" {
			failure(w, errors.New("An operator must reconcile the uncertain creation in Akash Console"))
			return
		}
		state, checkErr := s.Akash.State(r.Context(), row.Dseq)
		if checkErr != nil {
			failure(w, checkErr)
			return
		}
		row, err = s.Store.Transition(owner, row.ID, row.State, state, "Deployment reconciled against Akash; worker execution remains locked.", "")
	} else if action == "close" {
		if row.Dseq == "" || row.State == "closed" || row.State == "closing" {
			failure(w, ErrConflict)
			return
		}
		row, err = s.Store.Transition(owner, row.ID, row.State, "closing", "Account requested deployment closure.", "")
		if err != nil {
			failure(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
		defer cancel()
		if callErr := s.Akash.Close(ctx, row.Dseq); callErr != nil {
			row, err = s.Store.Transition(owner, row.ID, "closing", "reconcile", "Closure is unconfirmed. Check Akash Console; costs may continue.", "")
		} else {
			row, err = s.Store.Transition(owner, row.ID, "closing", "closure_pending", "Akash accepted closure. Refresh to confirm the deployment closed on chain.", "")
		}
	} else {
		w.WriteHeader(404)
		return
	}
	if err != nil {
		respond(w, 500, map[string]string{"error": "Outcome could not be persisted; contact the operator before retrying"})
		return
	}
	respond(w, 200, row)
}
