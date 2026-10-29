package cloud

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	bolt "go.etcd.io/bbolt"
)

var ErrNotFound = errors.New("Workspace not found")
var ErrConflict = errors.New("Workspace state changed; reload before continuing")

const reviewNote = "Akash automatically funds active deployments. A runtime limit closes the deployment after lease start; storage is not a backup. No files or keys are released before worker verification."

type Workspace struct {
	ID           string    `json:"id"`
	Owner        string    `json:"-"`
	Name         string    `json:"name"`
	State        string    `json:"state"`
	Dseq         string    `json:"dseq,omitempty"`
	RuntimeHours int       `json:"runtimeHours"`
	Image        string    `json:"image"`
	Manifest     string    `json:"manifest"`
	Review       string    `json:"review"`
	PlanHash     string    `json:"planHash"`
	Created      time.Time `json:"created"`
	Updated      time.Time `json:"updated"`
	Events       []Event   `json:"events"`
}
type storedWorkspace struct {
	Owner     string    `json:"owner"`
	Workspace Workspace `json:"workspace"`
}
type Event struct {
	At      time.Time `json:"at"`
	State   string    `json:"state"`
	Message string    `json:"message"`
}
type Store struct{ db *bolt.DB }

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("Cloud database must be a private regular file (mode 0600)")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	if err = db.Update(func(tx *bolt.Tx) error { _, err := tx.CreateBucketIfNotExists([]byte("workspaces")); return err }); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func ownerKey(owner, id string) []byte {
	sum := sha256.Sum256([]byte(owner))
	return []byte(hex.EncodeToString(sum[:]) + ":" + id)
}
func randomID() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func (s *Store) List(owner string) ([]Workspace, error) {
	out := []Workspace{}
	err := s.db.View(func(tx *bolt.Tx) error {
		prefix := ownerKey(owner, "")
		cursor := tx.Bucket([]byte("workspaces")).Cursor()
		for key, raw := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, raw = cursor.Next() {
			var row storedWorkspace
			if err := json.Unmarshal(raw, &row); err != nil {
				return err
			}
			if row.Owner != owner {
				return errors.New("Workspace owner index is inconsistent")
			}
			row.Workspace.Owner = owner
			out = append(out, row.Workspace)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, err
}
func (s *Store) Get(owner, id string) (Workspace, error) {
	var w Workspace
	err := s.db.View(func(tx *bolt.Tx) error { var err error; w, err = read(tx, owner, id); return err })
	return w, err
}
func read(tx *bolt.Tx, owner, id string) (Workspace, error) {
	var w storedWorkspace
	raw := tx.Bucket([]byte("workspaces")).Get(ownerKey(owner, id))
	if raw == nil {
		return Workspace{}, ErrNotFound
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return Workspace{}, err
	}
	if w.Owner != owner {
		return Workspace{}, ErrNotFound
	}
	w.Workspace.Owner = owner
	return w.Workspace, nil
}
func write(tx *bolt.Tx, w Workspace) error {
	raw, err := json.Marshal(storedWorkspace{w.Owner, w})
	if err != nil {
		return err
	}
	return tx.Bucket([]byte("workspaces")).Put(ownerKey(w.Owner, w.ID), raw)
}
func (s *Store) Plan(owner, name, image string, hours int) (Workspace, error) {
	if len(name) < 1 || len(name) > 60 || hours < 1 || hours > 24 {
		return Workspace{}, errors.New("Choose a name and a runtime between 1 and 24 hours")
	}
	manifest, err := SDL(image)
	if err != nil {
		return Workspace{}, err
	}
	now := time.Now().UTC()
	w := Workspace{ID: randomID(), Owner: owner, Name: name, State: "review", RuntimeHours: hours, Image: image, Manifest: manifest, Review: reviewNote, Created: now, Updated: now}
	hash := sha256.Sum256([]byte(w.ID + "\n" + manifest + "\n" + strconv.Itoa(hours)))
	w.PlanHash = hex.EncodeToString(hash[:])
	w.Events = []Event{{now, "review", "Review the resource plan before creating infrastructure."}}
	err = s.db.Update(func(tx *bolt.Tx) error {
		// One unresolved or active deployment per account bounds accidental spend.
		bucket := tx.Bucket([]byte("workspaces"))
		err := bucket.ForEach(func(_, raw []byte) error {
			var other storedWorkspace
			if err := json.Unmarshal(raw, &other); err != nil {
				return err
			}
			if other.Owner == owner && other.Workspace.State != "closed" && other.Workspace.State != "discarded" {
				return errors.New("Review or close your existing cloud workspace first")
			}
			return nil
		})
		if err != nil {
			return err
		}
		return write(tx, w)
	})
	return w, err
}
func (s *Store) Transition(owner, id, from, to, message, dseq string) (Workspace, error) {
	var w Workspace
	err := s.db.Update(func(tx *bolt.Tx) error {
		var err error
		w, err = read(tx, owner, id)
		if err != nil {
			return err
		}
		if w.State != from {
			return ErrConflict
		}
		if dseq != "" {
			w.Dseq = dseq
		}
		w.State = to
		w.Updated = time.Now().UTC()
		w.Events = append(w.Events, Event{w.Updated, to, message})
		return write(tx, w)
	})
	return w, err
}

// Recover marks interrupted mutations as uncertain. It never repeats a paid POST.
func (s *Store) Recover() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("workspaces"))
		var pending []Workspace
		err := bucket.ForEach(func(_, raw []byte) error {
			var r storedWorkspace
			if err := json.Unmarshal(raw, &r); err != nil {
				return err
			}
			w := r.Workspace
			w.Owner = r.Owner
			if w.State == "creating" || w.State == "closing" || w.State == "leasing" {
				w.State = "reconcile"
				w.Updated = time.Now().UTC()
				w.Events = append(w.Events, Event{w.Updated, "reconcile", "Service restarted during an infrastructure request. Operator reconciliation is required."})
				pending = append(pending, w)
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, w := range pending {
			if err := write(tx, w); err != nil {
				return err
			}
		}
		return nil
	})
}
