package state

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

const valid = `{"windows":{},"workbench":{},"messages":[]}`

func TestPersistsAndRejectsStaleWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Away OS", "workspace.json")
	s := New(path)
	v, e := s.Read()
	if e != nil || v.Revision != -1 {
		t.Fatal(v, e)
	}
	v, e = s.Commit(-1, valid)
	if e != nil || v.Revision != 0 {
		t.Fatal(v, e)
	}
	if _, e = s.Commit(-1, valid); e == nil {
		t.Fatal("stale write accepted")
	}
	reopened, e := New(path).Read()
	if e != nil || reopened.Revision != 0 || string(reopened.State) != valid {
		t.Fatal(reopened, e)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if _, e = s.Commit(0, `{"wrong":true}`); e == nil {
		t.Fatal("invalid shape accepted")
	}
}
func TestConcurrentWrites(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "workspace.json"))
	s.Commit(-1, valid)
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.Commit(0, valid); e == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("expected one winner, got %d", success)
	}
}
func TestCorruptionNeverReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.json")
	os.WriteFile(path, []byte("broken"), 0600)
	s := New(path)
	if _, e := s.Read(); e == nil {
		t.Fatal("corruption ignored")
	}
	if _, e := s.Commit(-1, valid); e == nil {
		t.Fatal("corruption overwritten")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "broken" {
		t.Fatal("data lost")
	}
}
