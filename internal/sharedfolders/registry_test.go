package sharedfolders

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBoundaryPersistenceAndPermissions(t *testing.T) {
	parent := t.TempDir()
	host := filepath.Join(parent, "project")
	os.Mkdir(host, 0755)
	os.WriteFile(filepath.Join(host, "note.txt"), []byte("original"), 0644)
	outside := filepath.Join(parent, "outside.txt")
	os.WriteFile(outside, []byte("private"), 0644)
	os.Symlink(outside, filepath.Join(host, "escape.txt"))
	config := filepath.Join(t.TempDir(), "folders.json")
	r, e := New(config)
	if e != nil {
		t.Fatal(e)
	}
	if e = r.Add("project", host, true); e != nil {
		t.Fatal(e)
	}
	p := Prefix + "/project/note.txt"
	read, e := r.Read(p, true)
	if e != nil || read.Entry.Content != "original" {
		t.Fatal(read, e)
	}
	if _, e = r.Write(p, "changed", "utf8", read.Entry.Version); e == nil {
		t.Fatal("read-only write accepted")
	}
	for _, bad := range []string{Prefix + "/project/../outside.txt", Prefix + "/project/escape.txt", "/etc/passwd", Prefix + "/unknown/note.txt"} {
		if _, e = r.Read(bad, true); e == nil {
			t.Fatal("boundary escaped", bad)
		}
	}
	if e = r.Mkdir(Prefix + "/project/new"); e == nil {
		t.Fatal("read-only mkdir accepted")
	}
	r.SetReadOnly("project", false)
	written, e := r.Write(p, "changed", "utf8", read.Entry.Version)
	if e != nil || written.Version == read.Entry.Version {
		t.Fatal(written, e)
	}
	if _, e = r.Write(p, "stale", "utf8", read.Entry.Version); e == nil {
		t.Fatal("stale save accepted")
	}
	if e = r.Mkdir(Prefix + "/project/data/nested"); e != nil {
		t.Fatal(e)
	}
	r2, e := New(config)
	if e != nil || len(r2.List()) != 1 || !r2.List()[0].Available {
		t.Fatal(r2, e)
	}
	info, _ := os.Stat(config)
	if info.Mode().Perm() != 0600 {
		t.Fatal("unsafe config permissions")
	}
	r2.Remove("project")
	if _, e = r2.Read(p, true); e == nil {
		t.Fatal("revoked grant still readable")
	}
	b, _ := os.ReadFile(filepath.Join(host, "note.txt"))
	if string(b) != "changed" {
		t.Fatal("unshare modified host files")
	}
}
func TestReplacedDirectoryDoesNotInheritGrant(t *testing.T) {
	parent := t.TempDir()
	host := filepath.Join(parent, "project")
	os.Mkdir(host, 0755)
	r, _ := New(filepath.Join(t.TempDir(), "folders.json"))
	r.Add("project", host, false)
	os.Rename(host, host+"-old")
	os.Mkdir(host, 0755)
	if r.List()[0].Available {
		t.Fatal("replacement inherited a grant")
	}
	if _, e := r.Read(Prefix+"/project", false); e == nil {
		t.Fatal("replacement readable")
	}
}
