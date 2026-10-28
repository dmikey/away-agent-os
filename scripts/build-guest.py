#!/usr/bin/env python3
"""Build a Linux-only initramfs from pinned upstream archives, never executing guest code."""
import pathlib,json,hashlib,tarfile,gzip,stat,struct,subprocess,os,zlib
root=pathlib.Path(__file__).resolve().parents[1]
lock=json.loads((root/'sandbox/macos-guest-lock.json').read_text());cache=pathlib.Path(os.environ.get('AWAY_GUEST_SOURCE_CACHE',root/'build/guest-sources'))
browser=os.environ.get("AWAY_BROWSER_IMAGE")=="1"
if browser:
 lock["assets"]=[a for a in lock["assets"] if not a["name"].startswith("cpython")]+json.loads((root/"sandbox/browser-guest-lock.json").read_text())["assets"]
entries={}
def add(name,mode,data=b'',link=''):
 name=name.removeprefix('./').rstrip('/')
 if not name or name.startswith('.') or name.startswith('/') or '..' in name.split('/'):return
 if link:data=link.encode()
 entries[name]=(mode,data)
for asset in lock['assets']:
 p=cache/asset['name'];assert hashlib.sha256(p.read_bytes()).hexdigest()==asset['sha256']
 with tarfile.open(p,ignore_zeros=True) as archive:
  members=archive.getmembers()
  for m in members:
   name=m.name.removeprefix('./')
   if asset['name'].startswith('linux-virt'):
    if name=='boot/vmlinuz-virt':
     target=root/'internal/macvm/assets/vmlinuz';kernel=archive.extractfile(m).read()
     if kernel[4:8]==b'zimg':
      start,size=struct.unpack_from('<II',kernel,8);kernel=zlib.decompress(kernel[start:start+size],31)
     assert kernel[56:60]==b'ARMd','Expected an uncompressed ARM64 Linux Image'
     target.write_bytes(kernel);continue
    if not name.startswith('lib/modules/'):continue
   elif asset['name'].startswith('cpython'):
    if not name.startswith('python/'):continue
    name='opt/'+name
   mode=(stat.S_IFDIR if m.isdir() else stat.S_IFLNK if m.issym() else stat.S_IFREG)|m.mode
   if m.isdir():add(name,mode)
   elif m.issym():add(name,mode,link=m.linkname)
   elif m.isfile() or m.islnk():add(name,mode,archive.extractfile(m).read())
# Required directories and initramfs console device.
for d in ['dev','proc','sys','run','tmp','workspace','opt','root']:add(d,stat.S_IFDIR|0o755)
# Busybox reads /etc/passwd for the unprivileged guest identity.
add('etc/passwd',stat.S_IFREG|0o644,b'root:x:0:0:root:/root:/bin/sh\nagent:x:1000:1000:Agent:/workspace:/bin/sh\nbrowser:x:2000:2000:Browser:/tmp/browser:/bin/false\n')
add('etc/group',stat.S_IFREG|0o644,b'root:x:0:\nagent:x:1000:\nbrowser:x:2000:\n')
add('agent_kernel.py',stat.S_IFREG|0o644,(root/'cmd/guest/agent_kernel.py').read_bytes())
add('init',stat.S_IFREG|0o755,(root/'internal/macvm/assets/guest-init').read_bytes())
# Ensure ancestors are explicit directory entries before child files.
for name in list(entries):
 for parent in pathlib.PurePosixPath(name).parents:
  if str(parent)!='.' and str(parent) not in entries:add(str(parent),stat.S_IFDIR|0o755)
output=bytearray()
def record(name,mode,data,ino,rdevmaj=0,rdevmin=0):
 fields=[ino,mode,0,0,1,0,len(data),0,0,rdevmaj,rdevmin,len(name.encode())+1,0]
 output.extend(b'070701'+b''.join(('%08x'%x).encode() for x in fields));output.extend(name.encode()+b'\0');output.extend(b'\0'*((-len(output))%4));output.extend(data);output.extend(b'\0'*((-len(output))%4))
for i,(name,(mode,data)) in enumerate(sorted(entries.items(),key=lambda x:(x[0].count('/'),x[0])),1):record(name,mode,data,i)
record('dev/console',stat.S_IFCHR|0o600,b'',len(entries)+1,5,1)
record('TRAILER!!!',0,b'',len(entries)+2)
assets=root/'internal/macvm/assets';image='browser-initramfs.gz' if browser else 'initramfs.gz';(assets/image).write_bytes(gzip.compress(output,compresslevel=6,mtime=0))
manifest={'backend':'apple-vz','arch':'arm64','files':{p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in [assets/'vmlinuz',assets/image]}}
(assets/('browser-manifest.json' if browser else 'manifest.json')).write_text(json.dumps(manifest,indent=2)+'\n')
print('Built pinned Linux guest:',len(output),'uncompressed bytes')
