//go:build !darwin

package macvm

import (
	"context"
	"errors"
	"github.com/dmikey/away-agent-os/desktop/internal/guestproto"
)

func (*Manager) BrowserAction(context.Context, string, guestproto.BrowserAction) (guestproto.BrowserFrame, error) {
	return guestproto.BrowserFrame{}, errors.New("macOS required")
}
func (*Manager) CloseBrowser(string) {}
