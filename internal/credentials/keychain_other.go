//go:build !darwin || !cgo

package credentials

import "errors"

type Keychain struct{}

func (Keychain) Save(string, string) error   { return errors.New("macOS Keychain unavailable") }
func (Keychain) Load(string) (string, error) { return "", errors.New("macOS Keychain unavailable") }
func (Keychain) Has(string) bool             { return false }
