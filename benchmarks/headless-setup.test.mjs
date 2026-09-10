import test from 'node:test';
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';

const script=fileURLToPath(new URL('../deploy/headless-next-boot.sh',import.meta.url));
function run(mode){
  // No root commands: the helper sees only this shell function. The restricted
  // PATH reproduces sudo lacking the developer's extension-provided ripgrep.
  return spawnSync('/bin/bash',['-c',`
    source "$1"
    systemctl() {
      case "$1" in
        show)
          if [[ "$2" == --property=LoadState ]]; then
            [[ "$MODE" != show-failure ]] || return 42
            if [[ "$MODE" == missing && "$4" == cups.service ]]; then
              printf 'not-found\\n'
            elif [[ "$MODE" == unknown ]]; then
              printf 'error\\n'
            else printf 'loaded\\n'; fi
          elif [[ "$2" == --property=UnitFileState ]]; then
            if [[ "$MODE" == still-enabled ]]; then printf 'enabled\\n'; else printf 'disabled\\n'; fi
          else return 90; fi ;;
        disable)
          [[ "$#" == 2 ]] || return 91
          [[ "$MODE" != disable-failure ]] || return 23
          printf 'DISABLE %s\\n' "$2" ;;
        *) printf 'Unexpected mutation: %s\\n' "$*" >&2; return 92 ;;
      esac
    }
    disable_optional_boot_units
    printf 'COMPLETE\\n'
  `,'headless-test',script],{encoding:'utf8',env:{PATH:'/usr/bin:/bin',MODE:mode}});
}
test('headless helper works without developer PATH and never stops active units',()=>{
  const r=run('ok');assert.equal(r.status,0,r.stderr);
  assert.equal(r.stdout.split('\n').filter(x=>x.startsWith('DISABLE ')).length,8);
  assert.match(r.stdout,/COMPLETE/);
});
test('headless helper skips genuinely uninstalled units only',()=>{
  const r=run('missing');assert.equal(r.status,0,r.stderr);
  assert.match(r.stdout,/Not installed, skipped: cups.service/);
  assert.equal(r.stdout.split('\n').filter(x=>x.startsWith('DISABLE ')).length,7);
});
for(const [mode,status] of [['show-failure',42],['disable-failure',23],['still-enabled',1],['unknown',1]]){
  test(`headless helper does not announce completion after ${mode}`,()=>{
    const r=run(mode);assert.equal(r.status,status,r.stderr);assert.doesNotMatch(r.stdout,/COMPLETE/);
  });
}
