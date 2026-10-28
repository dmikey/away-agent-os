package guestproto

import "testing"

func TestBrowserActionBoundary(t *testing.T) {
	for _, a := range []BrowserAction{{Kind: "navigate", URL: "file:///etc/passwd"}, {Kind: "navigate", URL: "https://user:pass@example.com/"}, {Kind: "navigate", URL: "https://example.com:8080"}, {Kind: "evaluate", Text: "process.exit()"}, {Kind: "type", Text: string(make([]byte, 4001))}, {Kind: "key", Key: "F12"}, {Kind: "resize", Width: 999999, Height: 700}, {Kind: "click", X: -1}} {
		if ValidateBrowser(a) == nil {
			t.Fatalf("accepted %#v", a)
		}
	}
	for _, a := range []BrowserAction{{Kind: "navigate", URL: "https://example.com/"}, {Kind: "read"}, {Kind: "click", X: 20, Y: 50}, {Kind: "scroll", Delta: 300}, {Kind: "type", Text: "hello"}, {Kind: "key", Key: "Enter"}} {
		if e := ValidateBrowser(a); e != nil {
			t.Fatal(e)
		}
	}
}
