// Package provider restricts credentials and requests to three fixed provider origins.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type Vault interface {
	Save(string, string) error
	Load(string) (string, error)
	Has(string) bool
}
type Client struct {
	Vault Vault
	HTTP  *http.Client
}
type Response struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
}
type Connection struct {
	Models []string `json:"models"`
}

var Names = []string{"OpenAI", "Anthropic", "xAI"}

func Origin(name string) (string, error) {
	switch name {
	case "OpenAI":
		return "https://api.openai.com/v1", nil
	case "Anthropic":
		return "https://api.anthropic.com/v1", nil
	case "xAI":
		return "https://api.x.ai/v1", nil
	}
	return "", errors.New("Choose OpenAI, Anthropic, or xAI")
}
func (c Client) Status() map[string]bool {
	s := map[string]bool{}
	for _, p := range Names {
		s[p] = c.Vault.Has(p)
	}
	return s
}
func (c Client) request(ctx context.Context, name, key, method, path, body string) (Response, error) {
	origin, e := Origin(name)
	if e != nil {
		return Response{}, e
	}
	key = strings.TrimSpace(key)
	if key == "" {
		key, e = c.Vault.Load(name)
		if e != nil {
			return Response{}, e
		}
	}
	if key == "" || len(key) > 2048 || strings.ContainsAny(key, "\r\n") {
		return Response{}, errors.New("Connect a provider key in agent settings")
	}
	req, e := http.NewRequestWithContext(ctx, method, origin+path, strings.NewReader(body))
	if e != nil {
		return Response{}, errors.New("Invalid provider request")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	if name == "Anthropic" {
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 46 * time.Second}
	}
	// Never forward a credential to a redirect destination, including in tests.
	guarded := *client
	guarded.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("Provider redirect rejected") }
	res, e := guarded.Do(req)
	if e != nil {
		return Response{}, errors.New("Could not reach the provider. Check your connection and try again.")
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, 4*1024*1024+1))
	if e != nil || len(b) > 4*1024*1024 {
		return Response{}, errors.New("Provider response too large")
	}
	if res.StatusCode >= 300 {
		return Response{Status: res.StatusCode, Body: `{"error":"Provider request failed"}`}, nil
	}
	return Response{Status: res.StatusCode, Body: string(b)}, nil
}
func Error(status int) error {
	switch status {
	case 401:
		return errors.New("The provider rejected this key. Copy a valid API key and try again.")
	case 403:
		return errors.New("This key does not have access. Check the provider project's API permissions.")
	case 429:
		return errors.New("Provider rate or spending limit reached. Check your API billing and try again.")
	}
	return errors.New("Provider request failed. Check API access and try again.")
}
func (c Client) Connect(ctx context.Context, name, key string) (Connection, error) {
	if strings.TrimSpace(key) == "" && !c.Vault.Has(name) {
		return Connection{}, errors.New("Paste an API key to continue")
	}
	res, e := c.request(ctx, name, key, "GET", "/models", "")
	if e != nil {
		return Connection{}, e
	}
	if res.Status != 200 {
		return Connection{}, Error(res.Status)
	}
	var data struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(res.Body), &data) != nil {
		return Connection{}, errors.New("The provider returned an invalid model list")
	}
	models := []string{}
	for _, m := range data.Data {
		if len(m.ID) < 150 && chatModel(name, m.ID) {
			models = append(models, m.ID)
		}
	}
	sort.Strings(models)
	if len(models) == 0 {
		return Connection{}, errors.New("No chat models are available to this key. Check your project's model permissions.")
	}
	if strings.TrimSpace(key) != "" {
		if e = c.Vault.Save(name, strings.TrimSpace(key)); e != nil {
			return Connection{}, e
		}
	}
	return Connection{Models: models}, nil
}
func chatModel(name, id string) bool {
	switch name {
	case "OpenAI":
		return (strings.HasPrefix(id, "gpt-") || strings.HasPrefix(id, "o1") || strings.HasPrefix(id, "o3") || strings.HasPrefix(id, "o4")) && !strings.Contains(id, "audio") && !strings.Contains(id, "realtime") && !strings.Contains(id, "transcribe") && !strings.Contains(id, "image") && !strings.Contains(id, "tts")
	case "Anthropic":
		return strings.HasPrefix(id, "claude-")
	case "xAI":
		return strings.HasPrefix(id, "grok-") && !strings.Contains(id, "image") && !strings.Contains(id, "video")
	}
	return false
}
func (c Client) Complete(ctx context.Context, name, key, body string) (Response, error) {
	if len(body) > 4*1024*1024 || !json.Valid([]byte(body)) {
		return Response{}, errors.New("Invalid provider request")
	}
	return c.request(ctx, name, key, "POST", "/chat/completions", body)
}
