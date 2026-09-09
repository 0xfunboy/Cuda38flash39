// Independent CPU-only test of the unedited Pi-delivered program, inside bwrap.
// Does not read credentials or call a model. Original Pi files remain read-only.
import fs from 'node:fs';
import crypto from 'node:crypto';
import {spawnSync} from 'node:child_process';
const report='/home/funboy/ai-exp/reports/moe-cluster/STRIX-PRODUCT-003';
const source=report+'/pi-project/nth_prime.c';
const receipt=report+'/pi-independent-prime-check.json';
if(fs.existsSync(receipt))throw Error('Receipt exists; do not overwrite evidence.');
const harness=String.raw`
import subprocess,json
limit=104729
sieve=bytearray(b'\x01')*(limit+1)
sieve[0:2]=b'\x00\x00'
for d in range(2,324):
 if sieve[d]:
  for c in range(d*d,limit+1,d): sieve[c]=0
primes=[i for i in range(limit+1) if sieve[i]]
checks=[]
for n in list(range(1,201))+[1000,9999,10000]:
 r=subprocess.run(['/work/nth_prime',str(n)],capture_output=True,text=True,timeout=3)
 checks.append({'argv':[str(n)],'expected':str(primes[n-1]),'stdout':r.stdout,'stderr':r.stderr,'exit':r.returncode,'passed':r.returncode==0 and r.stdout==str(primes[n-1])+'\n' and r.stderr==''})
for argv in [[],['abc'],['0'],['-1'],['10001'],['3.5'],[''],['10junk'],['999999999999999999999999999999999'],['1','2']]:
 r=subprocess.run(['/work/nth_prime']+argv,capture_output=True,text=True,timeout=3)
 checks.append({'argv':argv,'stdout':r.stdout,'stderr':r.stderr,'exit':r.returncode,'passed':r.returncode!=0 and r.stdout=='' and 'runtime error:' not in r.stderr and 'Sanitizer' not in r.stderr})
print(json.dumps({'oracle':'independent finite sieve through104729','checks':checks,'passed':all(c['passed'] for c in checks),'count':len(checks)}))
raise SystemExit(0 if all(c['passed'] for c in checks) else 1)
`;
const shell='ulimit -t 20; ulimit -c 0; gcc -std=c11 -O1 -Wall -Wextra -Werror -fsanitize=address,undefined -fno-sanitize-recover=all /source/nth_prime.c -o /work/nth_prime && exec python3 -c "$1"';
const args=['--unshare-all','--die-with-parent','--new-session','--ro-bind','/usr','/usr','--ro-bind','/bin','/bin','--ro-bind','/lib','/lib','--ro-bind','/lib64','/lib64','--ro-bind',source,'/source/nth_prime.c','--proc','/proc','--dev','/dev','--tmpfs','/tmp','--tmpfs','/work','--chdir','/work','--setenv','ASAN_OPTIONS','detect_leaks=0:abort_on_error=1','/bin/bash','-c',shell,'verify',harness];
const r=spawnSync('/usr/bin/bwrap',args,{encoding:'utf8',timeout:60000,maxBuffer:2*1024*1024,env:{PATH:'/usr/bin:/bin',LANG:'C.UTF-8'}});
let result;try{result=JSON.parse(r.stdout)}catch{result=null}
const out={observed_utc:new Date().toISOString(),source,sha256:crypto.createHash('sha256').update(fs.readFileSync(source)).digest('hex'),program_unedited:true,network:false,argv:['/usr/bin/bwrap',...args],exit:r.status,error:r.error?.message,stderr:r.stderr,result};
fs.writeFileSync(receipt,JSON.stringify(out,null,2)+'\n',{mode:0o600});
console.log(JSON.stringify({passed:r.status===0&&result?.passed,count:result?.count,exit:r.status,stderr:r.stderr,receipt},null,2));
if(r.status!==0||!result?.passed)process.exitCode=1;
