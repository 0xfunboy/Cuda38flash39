#!/usr/bin/env python3
"""Explicit, archived SYSTEM-CLEANUP-001 removal. No model/runtime deletion."""
import argparse
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import time

ROOT=Path('/home/funboy')
PRODUCT=ROOT/'StrixHaloClusterGLM'
REPORT=PRODUCT/'reports/SYSTEM-CLEANUP-001'


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--delete-archived-research',action='store_true',required=True)
    parser.parse_args()
    node={'01-EVO-X3':'node01','02-EVO-X3':'node02'}[socket.gethostname()]
    assert (REPORT/f'archive-{node}.sha256').is_file()
    moved=json.loads((REPORT/f'relocation-{node}-done.json').read_text())
    assert moved['status']=='PASS'
    if node=='node01':
        for path in [REPORT/'migration-api/summary.json',REPORT/'api-json/result.json']:
            value=json.loads(path.read_text())
            assert value.get('status')=='PASS' or value.get('phase')=='PASS', path
    for weight in moved['weights_unchanged']:
        st=(ROOT/'models/ciru-glm53-flash'/weight['relative']).stat()
        assert (st.st_ino,st.st_dev,st.st_size)==(weight['inode'],weight['device'],weight['size'])
    targets=[ROOT/'ai',ROOT/'ai-exp']
    if node=='node01': targets.append(ROOT/'code-gpt-main')
    # No active executable/cwd/open mapping may depend on the to-be-removed repos.
    uses=[]
    for proc in Path('/proc').iterdir():
        if not proc.name.isdigit() or int(proc.name)==os.getpid(): continue
        for p in [proc/'cwd',proc/'exe']:
            try: dest=os.readlink(p)
            except OSError: continue
            if any(dest==str(t) or dest.startswith(str(t)+'/') for t in targets): uses.append((proc.name,str(p),dest))
        try: maps=(proc/'maps').read_text()
        except OSError: continue
        if any(str(t)+'/' in maps for t in targets): uses.append((proc.name,'maps','old repository mapping'))
    if uses: raise RuntimeError('Live repository consumers: '+json.dumps(uses[:12]))
    # Retire only archived user unit files referring to the obsolete trees.
    unitroot=ROOT/'.config/systemd/user'
    unitfiles=[]
    for path in unitroot.rglob('*'):
        if path.suffix not in ('.service','.timer','.conf') or path.is_symlink() or not path.is_file(): continue
        if re.search(r'/home/funboy/ai(?:/|-exp/)',path.read_text()): unitfiles.append(path)
    units={p.name for p in unitfiles if p.parent==unitroot}
    units.update(p.parent.name.removesuffix('.d') for p in unitfiles if p.parent.name.endswith('.service.d'))
    timer=unitroot/'strix-vllm-recovery.timer'
    if timer.is_file(): units.add(timer.name); unitfiles.append(timer)
    for unit in sorted(units):
        state=subprocess.check_output(['systemctl','--user','show',unit,'-p','ActiveState','--value'],text=True).strip()
        if state in ('active','activating','deactivating','reloading'):
            raise RuntimeError('Unexpected active legacy unit: '+unit)
    journal={'started':time.time(),'node':node,'archives':str(PRODUCT/f'archives/research-{node}-20260910.tar.zst'),
             'targets':[str(p) for p in targets],'units':sorted(units),'removed':[],
             'recovery':'Research source/reports/configs in verified archive; excluded caches/installers can be regenerated. Models retained.'}
    out=REPORT/f'prune-{node}.json'
    assert not out.exists()
    def save(): out.write_text(json.dumps(journal,indent=2)+'\n')
    save()
    for unit in sorted(units):
        subprocess.run(['systemctl','--user','disable',unit],check=True,stdout=subprocess.DEVNULL)
    for path in unitfiles:
        path.unlink()
        journal['removed'].append(str(path));save()
    # Exact approved directories only. rm does not follow the models symlinks.
    for path in targets:
        assert path.parent==ROOT and path.name in ('ai','ai-exp','code-gpt-main')
        assert path.is_dir() and not path.is_symlink() and path.resolve()==path
        subprocess.run(['rm','-rf','--one-file-system','--',str(path)],check=True)
        journal['removed'].append(str(path));save()
    subprocess.run(['systemctl','--user','daemon-reload'],check=True)
    journal.update(status='PASS',finished=time.time());save()
    print(json.dumps({'status':'PASS','node':node,'repositories_removed':[str(p) for p in targets],'legacy_units_retired':len(units)}))


if __name__=='__main__':main()
