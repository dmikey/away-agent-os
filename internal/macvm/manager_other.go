//go:build !darwin

package macvm

import (
	"context"
	"errors"
	"github.com/dmikey/away-agent-os/desktop/internal/guestproto"
)

type Manager struct{}

func New() (*Manager, error) { return nil, errors.New("This build currently supports macOS only") }
func (*Manager) Execute(context.Context, string, guestproto.Job) (guestproto.Result, error) {
	return guestproto.Result{}, errors.New("macOS required")
}
func (*Manager) Reset() {}
func (*Manager) Close() {}

func (*Manager) OpenTerminal(context.Context, string, guestproto.Job, uint16, uint16) (*Terminal, error) {
	return nil, errors.New("macOS required")
}
