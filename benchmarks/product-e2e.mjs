#!/usr/bin/env node
// Real API regression: one verified apply on a disposable repository, followed
// by cancellation/draining of one live generation. Explicit opt-in required.
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import assert from 'node:assert/strict';
import {setTimeout as delay} from 'node:timers/promises';
import {fileURLToPath, pathToFileURL} from 'node:url';

export function parseOptions(argv){
  const o={endpoint:'http://127.0.0.1:18093',token_file:fileURLToPath(new URL('../state/api-token',import.meta.url))},pos=[];
  for(let i=0;i<argv.length;i++){
    const a=argv[i];
    if(a==='--run-live'){o.run_live=true;continue;}
    if(a==='--observe'){o.observe=true;continue;}
    if(a==='--endpoint'||a==='--token-file'){
      if(i+1>=argv.length)throw Error('Missing value for '+a);
      const key=a.slice(2).replaceAll('-','_');o[key]=argv[++i];o[key+'_explicit']=true;continue;
    }
    if(a.startsWith('--'))throw Error('Unknown option '+a);
    pos.push(a);
  }
  if(o.observe&& !o.run_live &&pos.length===1)o.output=path.resolve(pos[0]);
  else if(o.run_live&&!o.observe&&pos.length===2){o.spec_path=path.resolve(pos[0]);o.output=path.resolve(pos[1]);}
  else throw Error('Usage: product-e2e.mjs SPEC NEW_OUTPUT --run-live [--endpoint URL --token-file PATH], or OUTPUT --observe (GET-only)');
  return o;
}
export function loopbackEndpoint(value){
  const u=new URL(value);
  if(u.protocol!=='http:'||!['127.0.0.1','[::1]','localhost'].includes(u.hostname)||u.username||u.password||u.search||u.hash||u.pathname!=='/')throw Error('Endpoint must be a loopback HTTP origin without credentials or a path.');
  return u.origin;
}
export function tree(root){
  const result={};let bytes=0,files=0;
  function walk(dir){for(const entry of fs.readdirSync(dir,{withFileTypes:true}).sort((a,b)=>a.name.localeCompare(b.name))){const p=path.join(dir,entry.name);if(entry.isDirectory())walk(p);else if(entry.isFile()){const stat=fs.statSync(p);bytes+=stat.size;if(++files>512||bytes>64*1024*1024)throw Error('Fixture exceeds512files/64MiB; refusing an expansive repository copy.');result[path.relative(root,p)]=crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex');}else throw Error('Fixture symlink or special file refused: '+p);}}
  walk(root);return result;
}
export function disjointOutput(repo,output){
  const source=fs.realpathSync(repo),parent=fs.realpathSync(path.dirname(output));
  const destination=path.join(parent,path.basename(output));
  if(destination===source||destination.startsWith(source+path.sep)||source.startsWith(destination+path.sep))throw Error('Disposable output and original repository must be disjoint.');
}
export async function main(argv=process.argv.slice(2)){
const o=parseOptions(argv),output=o.output;
const previous=o.observe?JSON.parse(fs.readFileSync(path.join(output,'result.json'),'utf8')):null;
const endpoint=loopbackEndpoint(o.endpoint_explicit?o.endpoint:previous?.endpoint||o.endpoint);
const tokenFile=path.resolve(o.token_file_explicit?o.token_file:previous?.token_file||o.token_file);
const token=fs.readFileSync(tokenFile,'utf8').trim();if(!token)throw Error('Empty API token file.');
const redact=s=>String(s).split(token).join('[REDACTED]');
const report={started_utc:new Date().toISOString(),endpoint,token_file:tokenFile,checks:[],tasks:[],resume_argv:['node',fileURLToPath(import.meta.url),output,'--observe','--endpoint',endpoint,'--token-file',tokenFile]};
function write(name,value){const dest=path.join(output,name),temp=dest+'.tmp';fs.writeFileSync(temp,redact(JSON.stringify(value,null,2)),{mode:0o600});fs.renameSync(temp,dest);}
function save(){write('result.json',report);}
function check(name,condition){assert.ok(condition,name);report.checks.push({name,pass:true});save();}
async function api(method,route,body,authenticated=true){
  const r=await fetch(endpoint+route,{method,redirect:'error',credentials:'omit',headers:{'Content-Type':'application/json',...(authenticated?{Authorization:'Bearer '+token}:{})},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(15000)});
  const text=await r.text();let parsed;try{parsed=JSON.parse(text);}catch{throw Error(`Invalid API JSON HTTP ${r.status}: ${redact(text.slice(0,300))}`);}
  return {code:r.status,body:parsed};
}
if(o.observe){
  const observation={observed_utc:new Date().toISOString(),endpoint,read_only:true,prior_phase:previous.phase,manual_reconciliation_required:previous.status!=='PASS',warning:previous.status==='PASS'?'No new submissions.':'GET-only observation cannot resolve an accepted POST whose task ID was never received; reconcile the server journal before any new run.',tasks:[]};
  for(const id of previous.tasks||[]){if(!/^[a-f0-9]{32}$/.test(id))throw Error('Invalid saved task ID');observation.tasks.push({id,...await api('GET','/v1/coding/tasks/'+id)});}
  write('observation-'+Date.now()+'.json',observation);console.log(redact(JSON.stringify(observation,null,2)));return observation;
}
if(fs.existsSync(output))throw Error('Use a new output directory, or --observe to reconcile saved task IDs. Nothing is automatically resubmitted.');
const source=JSON.parse(fs.readFileSync(o.spec_path,'utf8'));
disjointOutput(source.repo,output);
const original=tree(source.repo);
fs.mkdirSync(output,{recursive:false,mode:0o700});report.source_spec=o.spec_path;report.original_hashes=original;save();
async function wait(id,seconds){
  if(!/^[a-f0-9]{32}$/.test(id))throw Error('Invalid task ID');
  const deadline=Date.now()+seconds*1000;
  const terminal=new Set(['PASS','FAIL','FAILED','ERROR','INCOMPLETE','BLOCKED','CANCELLED','CANCELED','INTERRUPTED']);
  while(Date.now()<deadline){
    const r=await api('GET','/v1/coding/tasks/'+id);
    if(r.code!==200||r.body.id!==id)throw Error('Task status unavailable; use recorded GET-only resume command.');
    report.last_observation={id,result:r.body};save();
    if(terminal.has(String(r.body.status).toUpperCase())){await delay(100);const latest=await api('GET','/v1/coding/tasks/'+id);if(latest.code!==200)throw Error('Terminal receipt refresh failed');if(terminal.has(String(latest.body.status).toUpperCase()))return latest.body;}
    await delay(2000);
  }
  throw Error('Task observation deadline; do not automatically resubmit.');
}
try {
  const health=await api('GET','/health');report.health_before=health;check('healthy preflight',health.code===200&&health.body.status==='ok');
  const status=await api('GET','/v1/status');report.status_before=status;check('idle preflight',status.code===200&&Array.isArray(status.body.active_request)&&status.body.active_request.length===0&&health.body.busy===false&&status.body.health?.busy===false);
  check('auth required',(await api('GET','/v1/models',undefined,false)).code===401);
  const models=await api('GET','/v1/models');check('OpenAI models',models.code===200&&models.body.data?.some(m=>m.id==='GLM5.3-Flash-CIRU-STRIX-IU4'));
  check('malformed chat rejected before ranks',(await api('POST','/v1/chat/completions',{messages:[{role:'user',content:'hello'}],max_tokens:-1})).code===400);
  const repo=path.join(output,'apply-repo');fs.cpSync(source.repo,repo,{recursive:true});
  const spec={...source,repo,profile:'fast',max_repairs:2,max_tokens:4096,timeout:600,apply:false,sandbox_policy:'isolated'};delete spec.test_timeout;
  const before=tree(repo);
  write('task-spec.json',spec);report.disposable_before=before;
  report.phase='SUBMIT_APPLY_INTENT';save();
  const submitted=await api('POST','/v1/coding/tasks',spec);report.submission=submitted;if(submitted.body.id)report.tasks.push(submitted.body.id);save();check('coding async202',submitted.code===202&&/^[a-f0-9]{32}$/.test(submitted.body.id));
  report.phase='OBSERVE_APPLY';save();
  const task=await wait(submitted.body.id,spec.timeout*(spec.max_repairs+1)+180);report.coding=task;save();
  check('real coding PASS',task.status==='PASS');
  check('compile/tests PASS',task.attempts.at(-1).build?.passed===true&&task.attempts.at(-1).tests?.passed===true&&!task.attempts.at(-1).build?.timed_out&&!task.attempts.at(-1).tests?.timed_out);
  check('original unchanged before apply',JSON.stringify(tree(repo))===JSON.stringify(before));
  check('apply needs explicit confirm',(await api('POST',`/v1/coding/tasks/${task.id}/apply`,{confirm:false})).code===400);
  report.phase='EXPLICIT_APPLY_INTENT';save();
  const applied=await api('POST',`/v1/coding/tasks/${task.id}/apply`,{confirm:true});
  report.apply_response=applied;save();check('explicit apply PASS',applied.code===200&&applied.body.status==='PASS'&&applied.body.applied===true);
  const after=tree(repo), changed=[...new Set([...Object.keys(before),...Object.keys(after)])].filter(n=>before[n]!==after[n]);
  check('applied only verified changed files',changed.length>0&&changed.every(n=>task.files_changed.includes(n)));
  check('frozen source remains untouched',JSON.stringify(tree(source.repo))===JSON.stringify(original));
  const cancelRepo=path.join(output,'cancel-repo');fs.cpSync(source.repo,cancelRepo,{recursive:true});
  const cancelSpec={...source,repo:cancelRepo,profile:'fast',max_repairs:0,max_tokens:1024,timeout:600,apply:false,sandbox_policy:'isolated'};delete cancelSpec.test_timeout;
  write('cancel-spec.json',cancelSpec);const cancelBefore=tree(cancelRepo);
  report.phase='SUBMIT_CANCEL_INTENT';save();
  const cancelSubmit=await api('POST','/v1/coding/tasks',cancelSpec);report.cancel_submission=cancelSubmit;const cancelID=cancelSubmit.body.id;if(cancelID)report.tasks.push(cancelID);save();
  check('cancel task accepted',cancelSubmit.code===202&&/^[a-f0-9]{32}$/.test(cancelID));
  let live=false;
  for(let n=0;n<100;n++){const h=await api('GET','/health');if(h.body.busy===true){live=true;break;}await delay(100);}
  check('generation was active before cancel',live);
  report.cancel_response=await api('POST',`/v1/coding/tasks/${cancelID}/cancel`,{});save();
  check('cancel accepted',report.cancel_response.code===202);
  const cancelled=await wait(cancelID,cancelSpec.timeout+180);report.cancelled=cancelled;save();
  check('cancel drains then terminates',cancelled.status==='CANCELLED'&&cancelled.model_calls===1&&!cancelled.applied);
  const healthAfter=await api('GET','/health');report.health_after=healthAfter;check('pair healthy and idle after cancellation',healthAfter.code===200&&healthAfter.body.status==='ok'&&healthAfter.body.busy===false);
  check('cancelled disposable repo unchanged',JSON.stringify(tree(cancelRepo))===JSON.stringify(cancelBefore));
  check('frozen repo untouched after cancel',JSON.stringify(tree(source.repo))===JSON.stringify(original));
  report.model_calls=task.model_calls+cancelled.model_calls;report.status='PASS';report.phase='COMPLETE';report.finished_utc=new Date().toISOString();save();
  console.log(JSON.stringify({status:report.status,checks:report.checks.length,tasks:report.tasks,raw:output},null,2));
} catch(e) {report.status='FAIL';report.error=redact(String(e));report.finished_utc=new Date().toISOString();save();throw e;}
return report;
}
if(process.argv[1]&&pathToFileURL(path.resolve(process.argv[1])).href===import.meta.url)main().catch(e=>{console.error(String(e));process.exitCode=1;});
