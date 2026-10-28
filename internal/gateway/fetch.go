package gateway

import (
	"context"
	"errors"
	"golang.org/x/net/html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Fetched struct {
	Links       []string `json:"links,omitempty"`
	Text        string   `json:"text"`
	ContentType string   `json:"contentType"`
	Status      int      `json:"status"`
	Truncated   bool     `json:"truncated"`
}

// Fetch lets agents request bounded readable text. TLS terminates for this explicit fetch only, never by interception.
func (p *Proxy) Fetch(ctx context.Context, actor, rawURL string, maxChars int) (Fetched, error) {
	u, e := url.Parse(rawURL)
	if e != nil || len(rawURL) > 2000 || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return Fetched{}, errors.New("Use an HTTP(S) URL without credentials or fragment")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	event := Event{Actor: actor, Host: host, Method: "FETCH", Status: 403, Decision: "blocked"}
	defer func() { p.record(event) }()
	if !domainPattern.MatchString(host) || !(u.Scheme == "https" && port == "443" || u.Scheme == "http" && port == "80") || !p.admit(actor, host) {
		return Fetched{}, errors.New("This domain is not approved in the actor's network policy")
	}
	defer p.release(actor)
	ctx, cancel := p.requestContext(ctx, actor, 30*time.Second)
	defer cancel()
	target, e := p.target(ctx, host, port)
	if e != nil {
		return Fetched{}, e
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return p.dial(ctx, "tcp", target)
	}, ResponseHeaderTimeout: 15 * time.Second, MaxResponseHeaderBytes: 65536, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if e != nil {
		return Fetched{}, e
	}
	req.Header.Set("User-Agent", "Away-OS-Workspace/0.1")
	response, e := client.Do(req)
	if e != nil {
		return Fetched{}, errors.New("Controlled fetch failed")
	}
	defer response.Body.Close()
	event.Status = response.StatusCode
	event.Decision = "allowed-text-fetch"
	body, e := io.ReadAll(io.LimitReader(response.Body, bodyLimit+1))
	if e != nil {
		return Fetched{}, e
	}
	event.Bytes = int64(len(body))
	truncated := int64(len(body)) > bodyLimit
	if truncated {
		body = body[:bodyLimit]
	}
	contentType := response.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/") && !strings.Contains(contentType, "json") {
		return Fetched{}, errors.New("Text fetch supports text, HTML and JSON; download binary assets inside the VM")
	}
	links := []string{}
	seenLinks := map[string]bool{}
	text := string(body)
	if strings.Contains(contentType, "html") {
		tokenizer := html.NewTokenizer(strings.NewReader(text))
		var result strings.Builder
		skip := 0
		for {
			token := tokenizer.Next()
			if token == html.ErrorToken {
				break
			}
			switch token {
			case html.StartTagToken, html.EndTagToken:
				tag, attrs := tokenizer.TagName()
				name := string(tag)
				if name == "a" && token == html.StartTagToken && skip == 0 && len(links) < 40 {
					for attrs {
						key, value, more := tokenizer.TagAttr()
						attrs = more
						if string(key) != "href" {
							continue
						}
						ref, err := url.Parse(string(value))
						if err != nil {
							continue
						}
						link := u.ResolveReference(ref)
						link.Fragment = ""
						h := strings.ToLower(link.Hostname())
						if (link.Scheme == "https" || link.Scheme == "http") && link.User == nil && len(link.String()) <= 2000 && domainPattern.MatchString(h) && net.ParseIP(h) == nil && !strings.HasSuffix(h, ".local") && !strings.HasSuffix(h, ".internal") && !seenLinks[link.String()] {
							links = append(links, link.String())
							seenLinks[link.String()] = true
						}
					}
				}
				if name == "script" || name == "style" || name == "noscript" {
					if token == html.StartTagToken {
						skip++
					} else if skip > 0 {
						skip--
					}
				}
			case html.TextToken:
				if skip == 0 {
					result.Write(tokenizer.Text())
					result.WriteByte(' ')
				}
			}
		}
		text = strings.Join(strings.Fields(result.String()), " ")
	}
	if maxChars <= 0 {
		maxChars = 12000
	}
	if maxChars > 50000 {
		maxChars = 50000
	}
	runes := []rune(text)
	if len(runes) > maxChars {
		text = string(runes[:maxChars])
		truncated = true
	}
	return Fetched{Links: links, Text: text, ContentType: contentType, Status: response.StatusCode, Truncated: truncated}, nil
}
