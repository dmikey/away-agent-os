package macvm

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

//go:embed assets/vmlinuz assets/initramfs.gz assets/manifest.json
var assets embed.FS

func prepareAssets() (string, error) {
	raw, e := assets.ReadFile("assets/manifest.json")
	if e != nil {
		return "", e
	}
	var m struct {
		Files map[string]string `json:"files"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return "", errors.New("Invalid guest manifest")
	}
	dir, e := os.MkdirTemp("", "away-vm-boot-")
	if e != nil {
		return "", e
	}
	ok := false
	defer func() {
		if !ok {
			os.RemoveAll(dir)
		}
	}()
	for _, name := range []string{"vmlinuz", "initramfs.gz"} {
		b, e := assets.ReadFile("assets/" + name)
		if e != nil {
			return "", e
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != m.Files[name] {
			return "", errors.New("Guest image integrity check failed")
		}
		if e = os.WriteFile(filepath.Join(dir, name), b, 0600); e != nil {
			return "", e
		}
	}
	ok = true
	return dir, nil
}
