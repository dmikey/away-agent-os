#!/usr/bin/env python3
"""Fetch source artifacts pinned in the guest lock; never execute them on macOS."""
import pathlib,json,hashlib,os,urllib.request
root=pathlib.Path(__file__).resolve().parents[1]
cache=pathlib.Path(os.environ.get('AWAY_GUEST_SOURCE_CACHE',root/'build/guest-sources'));cache.mkdir(parents=True,exist_ok=True)
for asset in json.loads((root/'sandbox/macos-guest-lock.json').read_text())['assets']+json.loads((root/'sandbox/browser-guest-lock.json').read_text())['assets']:
 target=cache/asset['name']
 if not target.exists():
  with urllib.request.urlopen(urllib.request.Request(asset['url'],headers={'User-Agent':'Away-OS-build'})) as source:data=source.read()
  if hashlib.sha256(data).hexdigest()!=asset['sha256']:raise RuntimeError('Guest source checksum mismatch')
  target.write_bytes(data)
 if hashlib.sha256(target.read_bytes()).hexdigest()!=asset['sha256']:raise RuntimeError('Guest source checksum mismatch: '+asset['name'])
 print(asset['name']+': verified')
