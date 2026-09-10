#!/usr/bin/env python3
"""One-time SYSTEM-CLEANUP-001 path relocation, never a numerical patch.

The old whole pair and gateway must be stopped first. Research deletion is a
separate explicit action after serving checks. This script never deletes repos.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import socket
import subprocess

PRODUCT = Path('/home/funboy/StrixHaloClusterGLM')
OLD = Path('/home/funboy/ai-exp/strix-ciru-tp2')
NEW = PRODUCT/'.engine'
MODELS = Path('/home/funboy/models/ciru-glm53-flash')
REPORT = PRODUCT/'reports/SYSTEM-CLEANUP-001'


def write(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open('x') as f: json.dump(value, f, indent=2)


def replace(path, pairs):
    data = path.read_bytes()
    changed = data
    for old, new in pairs: changed = changed.replace(old.encode(), new.encode())
    if changed == data: return None
    # Only caller-selected UTF-8 config, launchers and generated entrypoints.
    changed.decode('utf-8')
    path.write_bytes(changed)
    return dict(path=str(path), before=hashlib.sha256(data).hexdigest(),
                after=hashlib.sha256(changed).hexdigest())


def runtime(resume=False):
    node = {'01-EVO-X3':'node01', '02-EVO-X3':'node02'}[socket.gethostname()]
    archive = PRODUCT/f'archives/research-{node}-20260910.tar.zst'
    if not archive.is_file() or not (REPORT/f'archive-{node}.sha256').is_file():
        raise RuntimeError('Verified research archive required')
    units = ['ciru-model-rank0-001','ciru-model-rank1-001','ciru-frontend-001',
             'strixglm-rank0','strixglm-rank1','strixglm','strixglm-pair']
    for unit in units:
        state = subprocess.check_output(['systemctl','--user','show',unit,'-p','ActiveState','--value'],text=True).strip()
        if state in ('active','activating','deactivating','reloading'):
            raise RuntimeError('Service must be drained: '+unit)
    if resume:
        intent=json.loads((REPORT/f'relocation-{node}-intent.json').read_text())
        if OLD.exists() or not NEW.is_dir() or not MODELS.is_dir() or (NEW/'artifacts').resolve()!=MODELS:
            raise RuntimeError('Incomplete relocation is not the recorded moved layout')
        if (intent['old'],intent['new'],intent['models']) != (str(OLD),str(NEW),str(MODELS)):
            raise RuntimeError('Foreign relocation intent')
        weights=intent['weights']
    else:
        if not OLD.is_dir() or OLD.is_symlink() or NEW.exists() or MODELS.exists():
            raise RuntimeError('Unexpected relocation targets')
        weights = []
        for path in (OLD/'artifacts').rglob('*.safetensors'):
            st = path.stat()
            weights.append(dict(relative=str(path.relative_to(OLD/'artifacts')),
                                inode=st.st_ino, device=st.st_dev, size=st.st_size))
        if len(weights) != 12: raise RuntimeError('Expected 11 target parts plus drafter')
        write(REPORT/f'relocation-{node}-intent.json', dict(old=str(OLD),new=str(NEW),models=str(MODELS),weights=weights))
        (OLD/'artifacts').rename(MODELS)
        OLD.rename(NEW)
        (NEW/'artifacts').symlink_to(MODELS, target_is_directory=True)
    changes = []
    pairs = [(str(OLD),str(NEW))]
    paths = [NEW/'venv/pyvenv.cfg', NEW/'bootstrap/pyvenv.cfg']
    paths += [p for root in [NEW/'venv/bin',NEW/'bootstrap/bin'] if root.is_dir() for p in root.iterdir()
              if p.is_file() and not p.is_symlink() and p.stat().st_size < 4*1024*1024]
    for path in paths:
        if not path.is_file(): continue
        try: path.read_text()
        except UnicodeDecodeError: continue
        value = replace(path,pairs)
        if value: changes.append(value)
    for base in (NEW/'venv',NEW/'bootstrap'):
        for directory, names, files in os.walk(base,followlinks=False):
            for name in names+files:
                path = Path(directory)/name
                if path.is_symlink():
                    target = os.readlink(path)
                    if target.startswith(str(OLD)+'/'):
                        new_target = target.replace(str(OLD),str(NEW),1)
                        path.unlink()
                        path.symlink_to(new_target)
                        changes.append(dict(path=str(path),old_link=target,new_link=new_target))
    for item in weights:
        st = (MODELS/item['relative']).stat()
        if (st.st_ino,st.st_dev,st.st_size) != (item['inode'],item['device'],item['size']):
            raise RuntimeError('Weight identity changed')
    write(REPORT/f'relocation-{node}-done.json',dict(status='PASS',weights_unchanged=weights,changes=changes,
        scope='Only paths and symlinks; installed numerical Python/native files unmodified'))


def product():
    if socket.gethostname() != '01-EVO-X3': raise RuntimeError('Product metadata is node01 only')
    old_suite=Path('/home/funboy/ai-exp/reports/moe-cluster/STRIX-PRODUCT-001/suite')
    new_suite=PRODUCT/'runtime/fixtures/suite'
    shutil.copytree(old_suite,new_suite,symlinks=True)
    for path in new_suite.rglob('*.json'): replace(path,[(str(old_suite),str(new_suite))])
    historical=Path('/home/funboy/ai-exp/reports/moe-cluster/GLM-CIRU-OPT-004/g0/benchmark/protocol.json')
    shutil.copy2(historical,PRODUCT/'runtime/fixtures/historical-109-256.json')
    # Exact fixtures retain their source/test payload; re-pin path-only changes.
    source=PRODUCT/'internal/app/operations.go'
    text=source.read_text().replace(str(old_suite/'manifest.json'),str(new_suite/'manifest.json'))
    text=text.replace(str(historical),str(PRODUCT/'runtime/fixtures/historical-109-256.json'))
    text=re.sub(r'const operationSuiteSHA = "[0-9a-f]+"',
        'const operationSuiteSHA = "'+hashlib.sha256((new_suite/'manifest.json').read_bytes()).hexdigest()+'"',text)
    for task in new_suite.iterdir():
        if (task/'task.json').is_file():
            digest=hashlib.sha256((task/'task.json').read_bytes()).hexdigest()
            text=re.sub(r'("'+re.escape(task.name)+r'":\s*)"[0-9a-f]+"',lambda m:m[1]+'"'+digest+'"',text)
    source.write_text(text)
    pairs=[(str(OLD),str(NEW))]
    changes=[]
    selected=[PRODUCT/'runtime/cluster.json',PRODUCT/'runtime/manifest.json']
    selected += list((PRODUCT/'internal/app').glob('*.go'))
    selected += [PRODUCT/'config.json',PRODUCT/'config.native.json']
    for path in selected:
        if path.exists():
            value=replace(path,pairs)
            if value: changes.append(value)
    cfg=json.loads((PRODUCT/'config.json').read_text())
    cfg.update(backend='http://127.0.0.1:18094',tokenizer_endpoint='http://10.55.0.1:18110/tokenize',
               workspace_roots=[str(PRODUCT)])
    (PRODUCT/'config.json').write_text(json.dumps(cfg,indent=2)+'\n')
    # The gateway lock remains distinct from the native coordinator lock.
    assert cfg['pair_lock'] == str(NEW/'state/pair.lock')
    cat=PRODUCT/'runtime/model-catalog.json'
    replace(cat,[(str(OLD/'artifacts'),str(MODELS))])
    write(REPORT/'product-relocation.json',dict(status='PREPARED',changes=changes,
          fixtures='path-only metadata relocation with exact updated pins; same code and hidden tests',
          gateway_backend=cfg['backend'],weights_path=str(MODELS)))


if __name__=='__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('action',choices=['runtime','resume-runtime','product'])
    args=parser.parse_args()
    if args.action=='resume-runtime': runtime(resume=True)
    else: globals()[args.action]()
