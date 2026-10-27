#!/usr/bin/env python3
"""Download pinned Linux engines. Does not install, extract, execute, or start a VM."""
import hashlib,json,pathlib,urllib.request
root=pathlib.Path(__file__).resolve().parents[1]
lock=json.loads((root/'sandbox/firecracker-lock.json').read_text())
for asset in lock['assets']:
 target=root/'sandbox/assets'/asset['name']
 if not target.exists():
  request=urllib.request.Request(asset['url'],headers={'User-Agent':'Away-OS-build'})
  with urllib.request.urlopen(request) as source: data=source.read()
  assert hashlib.sha256(data).hexdigest()==asset['sha256'],'Firecracker checksum mismatch'
  target.write_bytes(data)
 assert hashlib.sha256(target.read_bytes()).hexdigest()==asset['sha256'],'Firecracker checksum mismatch'
 print(asset['name']+': checksum verified')
