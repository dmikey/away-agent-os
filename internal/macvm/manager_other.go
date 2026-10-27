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
