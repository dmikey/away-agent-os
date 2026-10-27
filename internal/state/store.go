// Package state keeps the local desktop workspace with optimistic revisions.
package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type Snapshot struct {
	State    json.RawMessage `json:"state"`
	Revision int             `json:"revision"`
}
type Store struct {
	mu   sync.Mutex
	path string
}

func New(path string) *Store { return &Store{path: path} }
func (s *Store) read() (Snapshot, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{Revision: -1}, nil
	}
	if err != nil {
		return Snapshot{}, err
	}
	var v Snapshot
	if err = json.Unmarshal(b, &v); err != nil {
		return v, errors.New("Workspace file is damaged; restore a backup before continuing")
	}
	return v, nil
}
func (s *Store) Read() (Snapshot, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.read() }
func (s *Store) Commit(expected int, raw string) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(raw) > 8*1024*1024 || !json.Valid([]byte(raw)) {
		return Snapshot{}, errors.New("Invalid or oversized workspace")
	}
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &object) != nil || object["windows"] == nil || object["workbench"] == nil {
		return Snapshot{}, errors.New("Invalid workspace shape")
	}
	prior, err := s.read()
	if err != nil {
		return prior, err
	}
	if prior.Revision != expected {
		return prior, errors.New("Revision conflict; retry")
	}
	next := Snapshot{State: json.RawMessage(raw), Revision: expected + 1}
	data, err := json.Marshal(next)
	if err != nil {
		return prior, err
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return prior, err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".workspace-*")
	if err != nil {
		return prior, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp, s.path)
	}
	if err != nil {
		return prior, err
	}
	return next, nil
}
