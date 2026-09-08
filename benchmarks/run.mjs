#!/usr/bin/env node
// Sequential real-product qualification. GET polling observes an asynchronous
// task; it never asks the model for status or submits duplicate generations.
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import {pathToFileURL} from 'node:url';
import {setTimeout as delay} from 'node:timers/promises';

export const terminal=new Set(['PASS','FAIL','INCOMPLETE','BLOCKED','CANCELLED','CANCELED','ERROR','FAILED','INTERRUPTED']);
export const effectiveReasoning=r=>r==='low'?'low':r==='high'?'high':'max';
export function median(values){const a=values.filter(Number.isFinite).sort((a,b)=>a-b);return a.length?a.length%2?a[(a.length-1)/2]:(a[a.length/2-1]+a[a.length/2])/2:null;}
export function aggregate(records){
  const groups={};
  for(const record of records){
    const r=record.result;if(!r||!terminal.has(String(r.status).toUpperCase()))continue;
    const profile=record.profile;
    const g=groups[profile]??={profile,tasks:0,pass:0,first_pass:0,repaired_pass:0,repairs:0,model_calls:0,wall_seconds:0,reasoning_tokens:0,final_tokens:0,completion_tokens:0,prompt_tokens:0,decode:[],ttft:[],acceptance:[],effective_reasoning:[],statuses:{}};
    g.tasks++;g.statuses[r.status]=(g.statuses[r.status]||0)+1;
    const attempts=r.attempts||[];const calls=Number(r.model_calls??attempts.length)||0;
    g.model_calls+=calls;g.repairs+=Math.max(0,attempts.length-1);g.wall_seconds+=Number(r.wall_seconds)||0;
    if(r.status==='PASS'){g.pass++;if(calls===1)g.first_pass++;else g.repaired_pass++;}
    for(const a of attempts){
      const m=a.metrics||a.model?.metrics||{};
      for(const key of ['reasoning_tokens','final_tokens','completion_tokens','prompt_tokens'])g[key]+=Number(m[key])||0;
      for(const [key,dest] of [['decode_tps','decode'],['ttft_ms','ttft'],['acceptance','acceptance']])if(typeof m[key]==='number')g[dest].push(m[key]);
      const e=a.effective_reasoning||effectiveReasoning(a.reasoning||profile);if(!g.effective_reasoning.includes(e))g.effective_reasoning.push(e);
    }
  }
  return Object.values(groups).map(({decode,ttft,acceptance,...g})=>({...g,decode_tps_median:median(decode),ttft_ms_median:median(ttft),draft_acceptance_median:median(acceptance),correct_solutions_per_hour:g.wall_seconds?g.pass*3600/g.wall_seconds:0,seconds_per_correct_including_failures:g.pass?g.wall_seconds/g.pass:null}));
}
export function loadManifest(file,seen=new Set()){
  const abs=path.resolve(file);if(seen.has(abs))throw Error('Manifest include cycle: '+abs);seen.add(abs);
  const m=JSON.parse(fs.readFileSync(abs,'utf8'));let tasks=[];
  for(const included of m.include_manifests||[])tasks.push(...loadManifest(path.resolve(path.dirname(abs),included),new Set(seen)).tasks);
  tasks.push(...(m.tasks||[]));
  return {...m,tasks:[...new Map(tasks.map(t=>[t.id,t])).values()]};
}
function save(file,value){fs.mkdirSync(path.dirname(file),{recursive:true});const temp=file+'.tmp';fs.writeFileSync(temp,JSON.stringify(value,null,2)+'\n',{mode:0o600});fs.renameSync(temp,file);}
function args(argv){const options={profiles:'low',endpoint:'http://127.0.0.1:18093',token_file:'/home/funboy/StrixHaloClusterGLM/state/api-token',max_repairs:'2',max_tokens:'4096',timeout:'600',poll_seconds:'5'};for(let i=0;i<argv.length;i++){if(argv[i]==='--dry-run'){options.dry_run=true;continue;}if(!argv[i].startsWith('--')||i+1>=argv.length)throw Error('Use --option value');options[argv[i].slice(2).replaceAll('-','_')]=argv[++i];}return options;}
function digest(v){return crypto.createHash('sha256').update(JSON.stringify(v)).digest('hex');}
function summarize(output){const records=fs.readdirSync(output).filter(n=>n.endsWith('.receipt.json')).map(n=>JSON.parse(fs.readFileSync(path.join(output,n),'utf8')));save(path.join(output,'summary.json'),{updated_utc:new Date().toISOString(),profiles:aggregate(records),records:records.map(r=>({id:r.fixture,profile:r.profile,task_id:r.task_id,status:r.result?.status||r.phase,wall_seconds:r.result?.wall_seconds,model_calls:r.result?.model_calls}))});}
export async function main(argv=process.argv.slice(2)){
  const o=args(argv);if(!o.suite||!o.output)throw Error('Required: --suite manifest.json --output NEW_OR_RESUME_DIR');
  const endpoint=new URL(o.endpoint);if(!['127.0.0.1','[::1]','localhost'].includes(endpoint.hostname)||endpoint.protocol!=='http:')throw Error('Benchmark endpoint must be loopback HTTP');
  const output=path.resolve(o.output);fs.mkdirSync(output,{recursive:true});
  const profiles=o.profiles.split(',');for(const p of profiles)if(!['low','medium','high','max','fast','balanced','quality'].includes(p))throw Error('Unknown profile '+p);
  const ids=o.ids?new Set(o.ids.split(',')):null;
  const all=[...new Map(o.suite.split(',').flatMap(f=>loadManifest(f).tasks).map(t=>[t.id,t])).values()];
  const tasks=all.filter(t=>!ids||ids.has(t.id));if(!tasks.length)throw Error('No tasks selected');
  if(ids&&tasks.length!==ids.size)throw Error('Some selected IDs were absent');
  const token=o.dry_run?'':fs.readFileSync(o.token_file,'utf8').trim();
  async function api(method,pathname,body){
    const response=await fetch(new URL(pathname,endpoint),{method,headers:{'Content-Type':'application/json','Authorization':'Bearer '+token},body:body?JSON.stringify(body):undefined,signal:AbortSignal.timeout(30000)});
    const text=await response.text();let result;try{result=JSON.parse(text);}catch{throw Error(`Invalid API JSON HTTP ${response.status}: ${text.slice(0,400)}`);}
    if(!response.ok)throw Error(`API HTTP ${response.status}: ${JSON.stringify(result).slice(0,1000)}`);return result;
  }
  // Interleave by task so reasoning profiles see comparable workload ordering.
  for(const fixture of tasks)for(const profile of profiles){
    const spec=JSON.parse(fs.readFileSync(fixture.spec,'utf8'));
    // The product's sandbox deadline is centrally configured, not a task field.
    delete spec.test_timeout;
    Object.assign(spec,{profile,max_repairs:Number(o.max_repairs),max_tokens:Number(o.max_tokens),timeout:Number(o.timeout)});
    if(o.context_tokens)spec.context_tokens=Number(o.context_tokens);
    const inputDigest=digest(spec),key=`${fixture.id}--${profile}`;
    if(!/^[a-zA-Z0-9_-]+$/.test(key))throw Error('Unsafe task/profile filename');
    const receiptPath=path.join(output,key+'.receipt.json');
    let record=fs.existsSync(receiptPath)?JSON.parse(fs.readFileSync(receiptPath,'utf8')):null;
    if(record&&record.input_sha256!==inputDigest)throw Error('Resume input mismatch for '+key+'; use a new output directory');
    if(record?.result&&terminal.has(String(record.result.status).toUpperCase())){console.log(`${key}: resume skips ${record.result.status}`);continue;}
    if(o.dry_run){console.log(`${key}: ${fixture.spec}; ${spec.files.length} files; no request sent`);continue;}
    if(record&&!record.task_id)throw Error(`Ambiguous previous submission ${receiptPath}; reconcile server task state before proceeding. It will NOT be resubmitted.`);
    if(!record){
      record={fixture:fixture.id,profile,phase:'SUBMIT_INTENT',started_utc:new Date().toISOString(),input_sha256:inputDigest,spec};
      if(o.resources==='true'){try{record.cluster_before=await api('GET','/v1/status');}catch(e){record.resource_error=String(e);}}
      save(receiptPath,record);
      const created=await api('POST','/v1/coding/tasks',spec);const taskId=created.id||created.task_id||created.task?.id;
      if(!taskId)throw Error('Task API response has no ID; ambiguous submission retained.');
      record.task_id=taskId;record.phase='SUBMITTED';record.submission=created;save(receiptPath,record);
    }
    console.log(`${key}: observing task ${record.task_id}`);
    let errors=0;
    for(;;){
      let result;try{result=await api('GET',`/v1/coding/tasks/${encodeURIComponent(record.task_id)}`);errors=0;}catch(e){if(++errors>=5)throw e;await delay(Number(o.poll_seconds)*1000);continue;}
      result=result.task||result;
      // A terminal status can become visible just before the worker's deferred
      // wall-time persistence. Do not turn that tiny race into a zero-time win.
      if(terminal.has(String(result.status).toUpperCase())&&result.wall_seconds===0){await delay(100);result=await api('GET',`/v1/coding/tasks/${encodeURIComponent(record.task_id)}`);result=result.task||result;}
      record.result=result;record.observed_utc=new Date().toISOString();record.phase=terminal.has(String(result.status).toUpperCase())?'TERMINAL':'OBSERVING';save(receiptPath,record);
      if(record.phase==='TERMINAL')break;
      await delay(Number(o.poll_seconds)*1000);
    }
    if(o.resources==='true'){try{record.cluster_after=await api('GET','/v1/status');}catch(e){record.resource_error=String(e);}save(receiptPath,record);}
    console.log(`${key}: ${record.result.status}; calls=${record.result.model_calls}; seconds=${record.result.wall_seconds}`);summarize(output);
  }
  if(!o.dry_run)summarize(output);
}
if(process.argv[1]&&import.meta.url===pathToFileURL(path.resolve(process.argv[1])).href)main().catch(e=>{console.error(e.stack||e);process.exitCode=1;});
