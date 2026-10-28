//go:build darwin

package macvm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/Code-Hex/vz/v3"
	"github.com/dmikey/away-agent-os/desktop/internal/guestproto"
	"io"
	"net/url"
	"time"
)

func (m *Manager) BrowserAction(ctx context.Context, actor string, a guestproto.BrowserAction) (guestproto.BrowserFrame, error) {
	if e := guestproto.ValidateBrowser(a); e != nil {
		return guestproto.BrowserFrame{}, e
	}
	if a.Kind == "navigate" {
		r, e := m.Gateway.RequestAccess(ctx, actor, a.URL, "Open this website in the isolated browser.")
		if e != nil {
			return guestproto.BrowserFrame{}, e
		}
		if r.Status != "approved" {
			return guestproto.BrowserFrame{}, errors.New("Approve the domain before navigating")
		}
	}
	s, e := m.sessionProfile(actor, true)
	if e != nil {
		return guestproto.BrowserFrame{}, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := "browser:" + actor
	var conn *vz.VirtioSocketConnection
	for until := time.Now().Add(45 * time.Second); time.Now().Before(until); {
		if ctx.Err() != nil {
			return guestproto.BrowserFrame{}, ctx.Err()
		}
		conn, e = s.vm.SocketDevices()[0].Connect(guestproto.BrowserPort)
		if e == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if conn == nil {
		m.drop(key, s)
		return guestproto.BrowserFrame{}, errors.New("Isolated browser did not start")
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(55 * time.Second))
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
			m.drop(key, s)
		case <-done:
		}
	}()
	if e = json.NewEncoder(conn).Encode(a); e != nil {
		m.drop(key, s)
		return guestproto.BrowserFrame{}, e
	}
	var frame guestproto.BrowserFrame
	if e = json.NewDecoder(io.LimitReader(conn, guestproto.BrowserMaxMessage)).Decode(&frame); e != nil {
		m.drop(key, s)
		return frame, errors.New("Browser exceeded its reply or time limit")
	}
	if frame.Error != "" {
		m.drop(key, s)
		return guestproto.BrowserFrame{}, errors.New(frame.Error)
	}
	u, e := url.Parse(frame.URL)
	if e != nil || len(frame.URL) > 2000 || u.Scheme != "http" && u.Scheme != "https" && frame.URL != "about:blank" || frame.Identity != "away-browser-vm/v1" || !frame.Sandboxed || len(frame.Text) > 120000 || len(frame.Title) > 1200 || len(frame.Links) > 40 || len(frame.Controls) > 60 || frame.Width < 480 || frame.Width > 1600 || frame.Height < 320 || frame.Height > 1000 {
		m.drop(key, s)
		return guestproto.BrowserFrame{}, errors.New("Invalid sandbox browser response")
	}
	for _, c := range frame.Controls {
		if len(c.Label) > 720 || len(c.Tag) > 100 || c.X < 0 || c.X > 1600 || c.Y < 0 || c.Y > 1000 {
			m.drop(key, s)
			return guestproto.BrowserFrame{}, errors.New("Invalid browser control")
		}
	}
	if frame.JPEG != "" {
		b, e := base64.StdEncoding.DecodeString(frame.JPEG)
		if e != nil || len(b) < 3 || b[0] != 0xff || b[1] != 0xd8 || b[2] != 0xff {
			m.drop(key, s)
			return guestproto.BrowserFrame{}, errors.New("Invalid browser frame")
		}
	}
	return frame, nil
}
func (m *Manager) CloseBrowser(actor string) {
	m.mu.Lock()
	s := m.sessions["browser:"+actor]
	m.mu.Unlock()
	if s != nil {
		m.drop("browser:"+actor, s)
	}
}
