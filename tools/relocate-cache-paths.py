#!/usr/bin/env python3
"""One-time cache path relocation after SYSTEM-CLEANUP-001 (pair stopped).

Preserve compiled kernels and tuning choices. Quarantine serialized graphs with
old filenames instead of editing pickle bytes. Back up every changed text file.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess

ROOT = Path('/home/funboy/StrixHaloClusterGLM')
CACHE = ROOT / '.engine/cache'
OLD = '/home/funboy/ai-exp/strix-ciru-tp2'
NEW = str(ROOT / '.engine')
node = {'01-EVO-X3': 'node01', '02-EVO-X3': 'node02'}[socket.gethostname()]
report = ROOT / 'reports/SYSTEM-CLEANUP-001'
backup = report / ('cache-paths-' + node)
os.umask(0o077)
for unit in ['strixglm', 'strixglm-pair', 'strixglm-rank0', 'strixglm-rank1']:
    state = subprocess.check_output(['systemctl', '--user', 'show', unit,
                                     '-p', 'ActiveState', '--value'], text=True).strip()
    if state in ('active', 'activating', 'deactivating', 'reloading'):
        raise RuntimeError('Drain the entire owned pair first: ' + unit)
if backup.exists():
    raise RuntimeError('Existing evidence: do not replay')
assert json.loads((report / ('prune-' + node + '.json')).read_text())['status'] == 'PASS'
backup.mkdir(mode=0o700)
changes = []

def saved(path, category):
    dst = backup / category / path.relative_to(CACHE)
    dst.parent.mkdir(parents=True, exist_ok=True)
    return dst

# These are generated cache metadata/wrappers, not installed runtime sources.
for path in CACHE.rglob('*'):
    if path.is_symlink() or not path.is_file() or path.suffix not in ('.py', '.json'):
        continue
    data = path.read_bytes()
    if OLD.encode() not in data:
        continue
    text = data.decode('utf-8')
    rewritten = text.replace(OLD, NEW).encode()
    shutil.copy2(path, saved(path, 'text-before'))
    path.write_bytes(rewritten)
    changes.append(dict(path=str(path.relative_to(CACHE)), action='text-path-only',
                        before=hashlib.sha256(data).hexdigest(),
                        after=hashlib.sha256(rewritten).hexdigest()))

# Serialized graphs can contain a complete old wrapper and its __file__. Do not
# unpickle/edit arbitrary objects. Let the same compiler rebuild these wrappers.
for base in [CACHE / 'torchinductor/fxgraph', CACHE / 'torchinductor/aotautograd']:
    for path in base.rglob('*'):
        if path.is_symlink() or not path.is_file():
            continue
        if OLD.encode() in path.read_bytes():
            path.rename(saved(path, 'serialized-graphs'))
            changes.append(dict(path=str(path.relative_to(CACHE)), action='quarantine-serialized-graph'))

# Only the nine small metadata files recreated by the previous startup may be
# removed from the old root. Preserve them too; never overwrite new tuning data.
residue = Path('/home/funboy/ai-exp')
files = list(residue.rglob('*')) if residue.exists() else []
for path in files:
    if path.is_symlink():
        raise RuntimeError('Unexpected symlink in old cache residue')
    if path.is_file():
        relative = path.relative_to(residue)
        if (not str(relative).startswith('strix-ciru-tp2/cache/torchinductor/')
                or path.suffix != '.best_config' or path.stat().st_size > 16384):
            raise RuntimeError('Unexpected old-root content: ' + str(path))
        data = json.loads(path.read_text())
        assert 'num_warps' in data and 'configs_hash' in data
        dst = backup / 'recreated-tuning' / relative
        dst.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, dst)
        current = Path(str(path).replace(OLD, NEW, 1))
        if not current.exists():
            current.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(path, current)
        changes.append(dict(path=str(relative), action='preserve-recreated-tuning',
                            same_as_current=path.read_bytes() == current.read_bytes()))
for path in files:
    if path.is_file():
        path.unlink()
for path in sorted(files, key=lambda p: len(p.parts), reverse=True):
    if path.is_dir():
        path.rmdir()
if residue.exists():
    residue.rmdir()
(backup / 'receipt.json').write_text(json.dumps(dict(status='PASS', node=node,
    scope='Cache path metadata only; compiled native kernels and tuning retained',
    changes=changes), indent=2) + '\n')
print(json.dumps(dict(status='PASS', node=node, text_paths=sum(x['action']=='text-path-only' for x in changes),
    quarantined_graphs=sum(x['action']=='quarantine-serialized-graph' for x in changes),
    old_root_absent=not residue.exists())))
