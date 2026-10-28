// Package sharedfolders is the human-owned grant registry and confined editor bridge.
package sharedfolders

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

const Prefix = "/workspace/Shared"
const MaxFile = 384000

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,59}$`)

type Grant struct {
	Name      string `json:"name"`
	HostPath  string `json:"hostPath"`
	ReadOnly  bool   `json:"readOnly"`
	Identity  string `json:"identity"`
	Available bool   `json:"available"`
	Path      string `json:"path"`
}
type Entry struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
	Version  int64  `json:"version"`
	Modified int64  `json:"modified"`
	Size     int64  `json:"size"`
	ReadOnly bool   `json:"readOnly"`
	Mounted  bool   `json:"mounted"`
}
type Listing struct {
	Entry   Entry   `json:"entry"`
	Entries []Entry `json:"entries,omitempty"`
}
type Registry struct {
	mu     sync.Mutex
	file   string
	grants []Grant
}

func New(file string) (*Registry, error) {
	r := &Registry{file: file}
	b, e := os.ReadFile(file)
	if errors.Is(e, os.ErrNotExist) {
		return r, nil
	}
	if e != nil {
		return nil, e
	}
	if json.Unmarshal(b, &r.grants) != nil {
		return nil, errors.New("Shared folder configuration is damaged")
	}
	for _, g := range r.grants {
		if !validName.MatchString(g.Name) || !filepath.IsAbs(g.HostPath) {
			return nil, errors.New("Invalid shared folder configuration")
		}
	}
	return r, nil
}
func identity(info os.FileInfo) string { return fmt.Sprintf("%v", fileIdentity(info)) }
func DefaultName(p string) string {
	s := strings.Trim(regexp.MustCompile(`[^a-z0-9_-]+`).ReplaceAllString(strings.ToLower(filepath.Base(p)), "-"), "-")
	if s == "" {
		s = "folder"
	}
	if len(s) > 50 {
		s = s[:50]
	}
	return s
}
func (r *Registry) List() []Grant {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]Grant{}, r.grants...)
	for i := range out {
		out[i].Path = Prefix + "/" + out[i].Name
		info, e := os.Lstat(out[i].HostPath)
		out[i].Available = e == nil && info.IsDir() && identity(info) == out[i].Identity
	}
	return out
}
func (r *Registry) save(next []Grant) error {
	if e := os.MkdirAll(filepath.Dir(r.file), 0700); e != nil {
		return e
	}
	b, e := json.Marshal(next)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(r.file), ".folders-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	f.Chmod(0600)
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		e = os.Rename(f.Name(), r.file)
	}
	if e == nil {
		r.grants = next
	}
	return e
}

// Add is called only with a path returned by the native picker (or disposable tests).
func (r *Registry) Add(name, host string, readOnly bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !validName.MatchString(name) {
		return errors.New("Use a folder name with lowercase letters, numbers, hyphens, or underscores")
	}
	if len(r.grants) >= 16 {
		return errors.New("Up to 16 folders can be shared")
	}
	host, e := filepath.EvalSymlinks(host)
	if e != nil {
		return e
	}
	host, e = filepath.Abs(host)
	if e != nil {
		return e
	}
	info, e := os.Lstat(host)
	if e != nil || !info.IsDir() {
		return errors.New("Choose an existing folder")
	}
	for _, g := range r.grants {
		if g.Name == name || g.HostPath == host {
			return errors.New("That folder or mount name is already shared")
		}
	}
	return r.save(append(append([]Grant{}, r.grants...), Grant{Name: name, HostPath: host, ReadOnly: readOnly, Identity: identity(info)}))
}
func (r *Registry) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := []Grant{}
	found := false
	for _, g := range r.grants {
		if g.Name == name {
			found = true
		} else {
			next = append(next, g)
		}
	}
	if !found {
		return errors.New("Shared folder not found")
	}
	return r.save(next)
}
func (r *Registry) SetReadOnly(name string, readOnly bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := append([]Grant{}, r.grants...)
	for i := range next {
		if next[i].Name == name {
			next[i].ReadOnly = readOnly
			return r.save(next)
		}
	}
	return errors.New("Shared folder not found")
}
func (r *Registry) resolve(p string) (Grant, *os.Root, string, error) {
	if path.Clean(p) != p || strings.ContainsAny(p, "\x00\\") || !strings.HasPrefix(p, Prefix+"/") {
		return Grant{}, nil, "", errors.New("Use a path inside an explicitly shared folder")
	}
	parts := strings.SplitN(strings.TrimPrefix(p, Prefix+"/"), "/", 2)
	for _, g := range r.grants {
		if g.Name == parts[0] {
			info, e := os.Lstat(g.HostPath)
			if e != nil || !info.IsDir() || identity(info) != g.Identity {
				return g, nil, "", errors.New("This folder is unavailable or has been replaced. Share it again to restore access.")
			}
			root, e := os.OpenRoot(g.HostPath)
			if e != nil {
				return g, nil, "", errors.New("Shared folder cannot be opened. Check its permissions or share it again.")
			}
			if e == nil {
				actual, err := root.Stat(".")
				if err != nil || identity(actual) != g.Identity {
					root.Close()
					return g, nil, "", errors.New("Shared folder identity changed. Select it again.")
				}
			}
			rel := "."
			if len(parts) == 2 {
				rel = parts[1]
			}
			return g, root, rel, e
		}
	}
	return Grant{}, nil, "", errors.New("That folder has not been shared")
}
func token(info os.FileInfo) int64 {
	s := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%v", info.ModTime().UnixNano(), info.Size(), fileIdentity(info))))
	var n int64
	for _, b := range s[:6] {
		n = n<<8 | int64(b)
	}
	return n
}
func entry(g Grant, p string, info os.FileInfo) Entry {
	k := "file"
	if info.IsDir() {
		k = "directory"
	}
	sum := sha256.Sum256([]byte(p))
	return Entry{ID: "mount-" + hex.EncodeToString(sum[:12]), Path: p, Kind: k, Encoding: "utf8", Version: token(info), Modified: info.ModTime().UnixMilli(), Size: info.Size(), ReadOnly: g.ReadOnly, Mounted: true}
}
func (r *Registry) Read(p string, content bool) (Listing, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, root, rel, e := r.resolve(p)
	if e != nil {
		return Listing{}, e
	}
	defer root.Close()
	info, e := root.Stat(rel)
	if e != nil {
		return Listing{}, e
	}
	out := Listing{Entry: entry(g, p, info)}
	if info.IsDir() {
		dir, e := root.Open(rel)
		if e != nil {
			return out, e
		}
		defer dir.Close()
		items, e := dir.ReadDir(501)
		if e != nil && e != io.EOF {
			return out, e
		}
		if len(items) > 500 {
			return out, errors.New("This folder has more than 500 entries. Browse it in the VM terminal.")
		}
		for _, item := range items {
			child := path.Join(rel, item.Name())
			i, e := root.Stat(child)
			if e != nil || (!i.IsDir() && !i.Mode().IsRegular()) {
				continue
			}
			out.Entries = append(out.Entries, entry(g, p+"/"+item.Name(), i))
		}
		sort.Slice(out.Entries, func(i, j int) bool {
			if out.Entries[i].Kind != out.Entries[j].Kind {
				return out.Entries[i].Kind == "directory"
			}
			return out.Entries[i].Path < out.Entries[j].Path
		})
		return out, nil
	}
	if !info.Mode().IsRegular() {
		return out, errors.New("Only regular files are editable")
	}
	if content {
		if info.Size() > MaxFile {
			return out, errors.New("This file is too large for the editor. It remains fully accessible inside the VM.")
		}
		f, e := root.Open(rel)
		if e != nil {
			return out, e
		}
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, MaxFile+1))
		if e != nil || len(b) > MaxFile {
			return out, errors.New("File exceeds the editor size limit")
		}
		if utf8.Valid(b) && !strings.ContainsRune(string(b), 0) {
			out.Entry.Content = string(b)
		} else {
			out.Entry.Content = base64.StdEncoding.EncodeToString(b)
			out.Entry.Encoding = "base64"
		}
	}
	return out, nil
}
func (r *Registry) Write(p, content, encoding string, expected int64) (Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, root, rel, e := r.resolve(p)
	if e != nil {
		return Entry{}, e
	}
	defer root.Close()
	if g.ReadOnly {
		return Entry{}, errors.New("This shared folder is read-only. Change access in Shared folders to allow edits.")
	}
	if rel == "." {
		return Entry{}, errors.New("Cannot replace a shared folder")
	}
	b := []byte(content)
	if encoding == "base64" {
		b, e = base64.StdEncoding.DecodeString(content)
	} else if encoding != "utf8" {
		return Entry{}, errors.New("Invalid file encoding")
	}
	if e != nil || len(b) > MaxFile {
		return Entry{}, errors.New("File exceeds the editor size limit")
	}
	old, e := root.Stat(rel)
	version := int64(0)
	mode := os.FileMode(0644)
	if e == nil {
		if !old.Mode().IsRegular() {
			return Entry{}, errors.New("Destination is not a regular file")
		}
		version = token(old)
		mode = old.Mode().Perm()
	} else if !errors.Is(e, os.ErrNotExist) {
		return Entry{}, e
	}
	if expected != version {
		return Entry{}, errors.New("File changed on disk. Reload it before saving.")
	}
	var random [8]byte
	rand.Read(random[:])
	tmp := path.Join(path.Dir(rel), ".away-"+hex.EncodeToString(random[:]))
	f, e := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if e != nil {
		return Entry{}, e
	}
	defer root.Remove(tmp)
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		e = root.Rename(tmp, rel)
	}
	if e != nil {
		return Entry{}, e
	}
	info, e := root.Stat(rel)
	if e != nil {
		return Entry{}, e
	}
	out := entry(g, p, info)
	out.Content = content
	out.Encoding = encoding
	return out, nil
}
func (r *Registry) Mkdir(p string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, root, rel, e := r.resolve(p)
	if e != nil {
		return e
	}
	defer root.Close()
	if g.ReadOnly {
		return errors.New("This shared folder is read-only")
	}
	if rel == "." {
		return nil
	}
	return root.MkdirAll(rel, 0755)
}
