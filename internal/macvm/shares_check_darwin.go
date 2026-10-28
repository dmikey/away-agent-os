//go:build darwin

package macvm

import (
	"context"
	"fmt"
	"github.com/dmikey/away-agent-os/desktop/internal/guestproto"
	"os"
	"path/filepath"
	"strings"
)

func (m *Manager) CheckShares(ctx context.Context) error {
	root, e := os.MkdirTemp("", "away-shares-check-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(root)
	for _, name := range []string{"rw", "ro"} {
		os.Mkdir(filepath.Join(root, name), 0755)
		os.WriteFile(filepath.Join(root, name, "hello.txt"), []byte("hello"), 0644)
	}
	os.WriteFile(filepath.Join(root, "outside.txt"), []byte("outside"), 0644)
	os.Symlink(filepath.Join(root, "outside.txt"), filepath.Join(root, "rw", "escape"))
	os.WriteFile(filepath.Join(root, "rw", "large.dat"), make([]byte, 900000), 0644)
	os.WriteFile(filepath.Join(root, "rw", "helper.py"), []byte("value=123"), 0644)
	m.SetShares([]Share{{Name: "write", Path: filepath.Join(root, "rw")}, {Name: "read", Path: filepath.Join(root, "ro"), ReadOnly: true}})
	defer m.SetShares(nil)
	r, e := m.Execute(ctx, "shares-check", guestproto.Job{ID: "share-test", Kind: "python", FilePath: "/workspace/Shared/write/main.py", Files: []guestproto.File{{Path: "/workspace/virtual.txt", Kind: "file", Encoding: "utf8", Content: "virtual boundary file"}, {Path: "/workspace/Shared/write/hello.txt", Kind: "file", Encoding: "utf8", Content: "must not overwrite mount"}}, Source: `import os,helper
assert helper.value==123
print(os.listdir('/workspace'))
print(os.listdir('/workspace/Shared'))
p='/workspace/Shared/write'
print('ownership',os.stat(p).st_uid,os.stat(p).st_gid,oct(os.stat(p).st_mode))
assert open(p+'/hello.txt').read()=='hello'
assert open('/workspace/Shared/read/hello.txt').read()=='hello'
assert open('/workspace/virtual.txt').read()=='virtual boundary file'
assert len(open(p+'/large.dat','rb').read())==900000
assert not os.path.exists(p+'/escape')
open(p+'/new.txt','w').write('guest write')
try:
 open('/workspace/Shared/read/no.txt','w').write('must fail')
except OSError: pass
else: raise AssertionError('read-only share was writable')
print('SHARED_MOUNTS_OK')`})
	fmt.Println("SHARED_VM_OUTPUT", r.Output)
	if e != nil {
		return e
	}
	if r.Error {
		return fmt.Errorf("shared guest failed")
	}
	for _, f := range r.Files {
		if strings.HasPrefix(f.Path, "/workspace/Shared") {
			return fmt.Errorf("mount leaked into snapshot export")
		}
	}
	original, _ := os.ReadFile(filepath.Join(root, "rw/hello.txt"))
	if string(original) != "hello" {
		return fmt.Errorf("snapshot import overwrote mount")
	}
	data, e := os.ReadFile(filepath.Join(root, "rw/new.txt"))
	if e != nil || string(data) != "guest write" {
		return fmt.Errorf("host write was not visible: %v", e)
	}
	r, e = m.Execute(ctx, "shares-check", guestproto.Job{ID: "share-again", Kind: "python", Source: "assert open('/workspace/Shared/write/new.txt').read()=='guest write'\nprint('LIVE_MOUNT_SURVIVED_WORKSPACE_REFRESH')"})
	if e != nil || r.Error {
		return fmt.Errorf("mount persistence: %v %s", e, r.Output)
	}
	fmt.Print(r.Output)
	other, e := m.Execute(ctx, "another-agent", guestproto.Job{ID: "share-other-agent", Kind: "python", Source: "assert open('/workspace/Shared/write/new.txt').read()=='guest write';print('SHARED_BOUNDARY_VISIBLE_TO_OTHER_AGENT')"})
	if e != nil || other.Error {
		return fmt.Errorf("other agent boundary: %v %s", e, other.Output)
	}
	fmt.Print(other.Output)
	m.SetShares(nil)
	r, e = m.Execute(ctx, "shares-check", guestproto.Job{ID: "share-revoked", Kind: "python", Files: []guestproto.File{{Path: "/workspace/Shared/ordinary.txt", Kind: "file", Encoding: "utf8", Content: "ordinary virtual file"}}, Source: "import os;assert not os.path.exists('/workspace/Shared/write');assert open('/workspace/Shared/ordinary.txt').read()=='ordinary virtual file';print('MOUNT_REVOCATION_AND_FULL_VFS_BOUNDARY_OK')"})
	if e != nil || r.Error {
		return fmt.Errorf("revocation: %v %s", e, r.Output)
	}
	fmt.Print(r.Output)
	return nil
}
