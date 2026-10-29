// Package cloud provides Away's operator-owned infrastructure control plane.
// Akash credentials never enter a workspace, SDL manifest, or desktop response.
package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

const AkashURL = "https://console-api.akash.network"

var sequence = regexp.MustCompile(`^[0-9]{1,30}$`)
var projectID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var imageDigest = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9./_-]+@sha256:[a-f0-9]{64}$`)

type Akash struct {
	Key, Project string
	Client       *http.Client
}
type apiError struct{ Status int }

func (e apiError) Error() string {
	return fmt.Sprintf("Akash returned HTTP %d; review the operator account", e.Status)
}
func (a *Akash) call(ctx context.Context, method, path string, body, out any) error {
	if a.Key == "" || !projectID.MatchString(a.Project) {
		return errors.New("Akash project credential is not configured")
	}
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, AkashURL+path, &payload)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", a.Key)
	req.Header.Set("x-project-id", a.Project)
	req.Header.Set("Content-Type", "application/json")
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("Akash request did not complete; reconcile before retrying a mutation")
	}
	defer res.Body.Close()
	// Never return upstream error bodies: they can contain manifests or credentials.
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return apiError{res.StatusCode}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(out)
}
func (a *Akash) Check(ctx context.Context) error {
	return a.call(ctx, "GET", "/v1/deployments?limit=1", nil, &json.RawMessage{})
}
func (a *Akash) Create(ctx context.Context, sdl string, hours int) (string, error) {
	var out struct {
		Data struct {
			Dseq string `json:"dseq"`
		} `json:"data"`
	}
	err := a.call(ctx, "POST", "/v1/deployments", map[string]any{"data": map[string]any{"sdl": sdl, "projectId": a.Project, "runtimeLimitHours": hours}}, &out)
	if err != nil {
		return "", err
	}
	if !sequence.MatchString(out.Data.Dseq) {
		return "", errors.New("Akash returned no deployment ID; reconcile before retrying")
	}
	return out.Data.Dseq, nil
}
func (a *Akash) Inspect(ctx context.Context, dseq string) (json.RawMessage, error) {
	if !sequence.MatchString(dseq) {
		return nil, errors.New("Invalid deployment ID")
	}
	var out json.RawMessage
	err := a.call(ctx, "GET", "/v1/deployments/"+url.PathEscape(dseq), nil, &out)
	return out, err
}
func (a *Akash) Close(ctx context.Context, dseq string) error {
	if !sequence.MatchString(dseq) {
		return errors.New("Invalid deployment ID")
	}
	return a.call(ctx, "DELETE", "/v1/deployments/"+dseq, nil, nil)
}

// SDL requests a confidential VM and contains no user data, credentials or SSH.
// Requesting TEE is not proof of attestation. The control plane never marks this ready.
func SDL(image string) (string, error) {
	if !imageDigest.MatchString(image) {
		return "", errors.New("Set a public worker image pinned by sha256 digest")
	}
	return fmt.Sprintf(`version: "2.1"
services:
  away:
    image: %s
    params:
      tee: cpu
    expose:
      - port: 8443
        as: 443
        to:
          - global: true
profiles:
  compute:
    away:
      resources:
        cpu:
          units: 2
        memory:
          size: 4Gi
        storage:
          - size: 10Gi
  placement:
    confidential:
      pricing:
        away:
          denom: uact
          amount: 1000
deployment:
  away:
    confidential:
      profile: away
      count: 1
`, image), nil
}

type Bid struct {
	ID struct {
		Dseq     string `json:"dseq"`
		Gseq     int    `json:"gseq"`
		Oseq     int    `json:"oseq"`
		Provider string `json:"provider"`
	} `json:"id"`
	State string `json:"state"`
	Price struct {
		Denom  string `json:"denom"`
		Amount string `json:"amount"`
	} `json:"price"`
}

func (a *Akash) Bids(ctx context.Context, dseq string) ([]Bid, error) {
	if !sequence.MatchString(dseq) {
		return nil, errors.New("Invalid deployment ID")
	}
	var out struct {
		Data []struct {
			Bid Bid `json:"bid"`
		} `json:"data"`
	}
	err := a.call(ctx, "GET", "/v1/bids?dseq="+dseq, nil, &out)
	bids := []Bid{}
	for _, b := range out.Data {
		if b.Bid.State == "open" && b.Bid.ID.Dseq == dseq {
			bids = append(bids, b.Bid)
		}
	}
	return bids, err
}
func (a *Akash) Lease(ctx context.Context, bid Bid) error {
	return a.call(ctx, "POST", "/v1/leases", map[string]any{"leases": []any{bid.ID}}, nil)
}

// State only returns bounded lifecycle metadata, never provider-controlled URLs.
func (a *Akash) State(ctx context.Context, dseq string) (string, error) {
	raw, err := a.Inspect(ctx, dseq)
	if err != nil {
		return "", err
	}
	var out struct {
		Data struct {
			Deployment struct {
				ID struct {
					Dseq string `json:"dseq"`
				} `json:"id"`
				State string `json:"state"`
			} `json:"deployment"`
			Leases []json.RawMessage `json:"leases"`
		} `json:"data"`
	}
	if err = json.Unmarshal(raw, &out); err != nil || out.Data.Deployment.ID.Dseq != dseq {
		return "", errors.New("Akash deployment identity did not match")
	}
	if out.Data.Deployment.State == "closed" {
		return "closed", nil
	}
	if out.Data.Deployment.State != "active" {
		return "", errors.New("Deployment state is not yet confirmed")
	}
	if len(out.Data.Leases) > 0 {
		return "verification_required", nil
	}
	return "bidding", nil
}
