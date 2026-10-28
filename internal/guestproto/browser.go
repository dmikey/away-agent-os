package guestproto

import (
	"errors"
	"net/url"
	"strings"
)

const BrowserPort = 1027
const BrowserMaxMessage = 3 * 1024 * 1024

// BrowserAction is deliberately narrower than CDP. Page code never reaches a native bridge.
type BrowserAction struct {
	Kind   string `json:"kind"`
	URL    string `json:"url,omitempty"`
	Text   string `json:"text,omitempty"`
	Key    string `json:"key,omitempty"`
	X      int    `json:"x,omitempty"`
	Y      int    `json:"y,omitempty"`
	Delta  int    `json:"delta,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}
type BrowserControl struct {
	Label string `json:"label"`
	Tag   string `json:"tag"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
}
type BrowserFrame struct {
	Controls  []BrowserControl `json:"controls,omitempty"`
	Identity  string           `json:"identity"`
	URL       string           `json:"url"`
	Title     string           `json:"title"`
	Text      string           `json:"text"`
	Links     []string         `json:"links"`
	JPEG      string           `json:"jpeg,omitempty"`
	Width     int              `json:"width"`
	Height    int              `json:"height"`
	Sandboxed bool             `json:"sandboxed"`
	Error     string           `json:"error,omitempty"`
}

func ValidateBrowser(a BrowserAction) error {
	switch a.Kind {
	case "navigate":
		u, e := url.Parse(a.URL)
		if e != nil || len(a.URL) > 2000 || u.User != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
			return errors.New("Only public HTTP(S) navigation is supported")
		}
		if p := u.Port(); p != "" && !(u.Scheme == "https" && p == "443" || u.Scheme == "http" && p == "80") {
			return errors.New("Only standard web ports are supported")
		}
	case "frame", "read", "reload":
	case "click", "scroll":
		if a.X < 0 || a.X > 1600 || a.Y < 0 || a.Y > 1000 || a.Delta < -2000 || a.Delta > 2000 {
			return errors.New("Invalid browser coordinates")
		}
	case "type":
		if len(a.Text) > 4000 || strings.ContainsRune(a.Text, 0) {
			return errors.New("Browser input is too large")
		}
	case "key":
		switch a.Key {
		case "Enter", "Backspace", "Tab", "Escape", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "Delete", "Home", "End":
		default:
			return errors.New("Unsupported browser key")
		}
	case "resize":
		if a.Width < 480 || a.Width > 1600 || a.Height < 320 || a.Height > 1000 {
			return errors.New("Invalid browser viewport")
		}
	default:
		return errors.New("Unsupported browser action")
	}
	return nil
}
