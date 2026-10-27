package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type vault map[string]string

func (v vault) Save(p, k string) error        { v[p] = k; return nil }
func (v vault) Load(p string) (string, error) { return v[p], nil }
func (v vault) Has(p string) bool             { return v[p] != "" }

type transport func(*http.Request) (*http.Response, error)

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) { return t(r) }
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}
func TestConnectAndCompleteAllProviders(t *testing.T) {
	for _, name := range Names {
		t.Run(name, func(t *testing.T) {
			v := vault{}
			calls := 0
			host, _ := Origin(name)
			model := map[string]string{"OpenAI": "gpt-4.1-mini", "Anthropic": "claude-sonnet-4-5", "xAI": "grok-4-fast"}[name]
			client := Client{Vault: v, HTTP: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != host+map[string]string{"GET": "/models", "POST": "/chat/completions"}[r.Method] {
					t.Fatal("unexpected endpoint", r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer fixture-key" {
					t.Fatal("missing credential")
				}
				if name == "Anthropic" && r.Header.Get("anthropic-version") == "" {
					t.Fatal("missing version")
				}
				if r.Method == "GET" {
					return reply(200, `{"data":[{"id":"`+model+`"},{"id":"text-embedding-3-small"}]}`), nil
				}
				return reply(200, `{"choices":[{"message":{"content":"hello"}}]}`), nil
			})}}
			connection, e := client.Connect(context.Background(), name, "fixture-key")
			if e != nil || len(connection.Models) != 1 || connection.Models[0] != model {
				t.Fatal(connection, e)
			}
			if v[name] != "fixture-key" || !client.Status()[name] {
				t.Fatal("key not saved")
			}
			if _, e = client.Connect(context.Background(), name, ""); e != nil {
				t.Fatal("saved key reconnect", e)
			}
			result, e := client.Complete(context.Background(), name, "", `{"model":"`+model+`","messages":[]}`)
			if e != nil || result.Status != 200 || calls != 3 {
				t.Fatal(result, e, calls)
			}
		})
	}
}
func TestRejectedCredentialsDoNotReplaceSavedKeys(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		v := vault{"OpenAI": "previous"}
		c := Client{Vault: v, HTTP: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
			return reply(status, `{"error":"secret request details"}`), nil
		})}}
		_, e := c.Connect(context.Background(), "OpenAI", "invalid")
		if e == nil || strings.Contains(e.Error(), "secret") || v["OpenAI"] != "previous" {
			t.Fatal("credential/error leak", e)
		}
	}
}
func TestProviderBoundary(t *testing.T) {
	v := vault{}
	calls := 0
	c := Client{Vault: v, HTTP: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls++
		res := reply(302, "")
		res.Header.Set("Location", "https://attacker.invalid")
		return res, nil
	})}}
	if _, e := c.Complete(context.Background(), "https://attacker.invalid", "fixture-key", `{}`); e == nil || calls != 0 {
		t.Fatal("arbitrary destination accepted")
	}
	if _, e := c.Connect(context.Background(), "OpenAI", "fixture-key"); e == nil || calls != 1 || len(v) != 0 {
		t.Fatal("redirect followed or key saved")
	}
	if _, e := c.Complete(context.Background(), "OpenAI", "fixture-key", "invalid"); e == nil {
		t.Fatal("invalid JSON accepted")
	}
}
