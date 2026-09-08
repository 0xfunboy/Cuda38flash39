#!/usr/bin/env node
// One explicitly authorized JSON chat completion. No retry, repair, lifecycle
// action or automatic recovery. This is an API smoke, not a coding benchmark.
import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
import {fileURLToPath, pathToFileURL} from 'node:url';
import {loopbackEndpoint} from './product-e2e.mjs';

export const MODEL='GLM5.3-Flash-CIRU-STRIX-IU4';
export function options(argv){
  const o={endpoint:'http://127.0.0.1:18093',token_file:fileURLToPath(new URL('../state/api-token',import.meta.url))};
  for(let i=0;i<argv.length;i++){
    const key=argv[i];
    if(key==='--run-live'){o.run_live=true;continue;}
    if(!['--endpoint','--token-file','--output'].includes(key)||i+1>=argv.length)throw Error('Use --endpoint URL --token-file PATH --output NEW_DIR --run-live');
    o[key.slice(2).replaceAll('-','_')]=argv[++i];
  }
  if(!o.run_live||!o.output)throw Error('Explicit --run-live and a new --output directory are required.');
  o.endpoint=loopbackEndpoint(o.endpoint);o.output=path.resolve(o.output);o.token_file=path.resolve(o.token_file);
  return o;
}

export function validateCompletion(body){
  assert.equal(body?.object,'chat.completion','OpenAI JSON chat.completion object');
  assert.equal(body.model,MODEL,'pinned model identity');
  assert.equal(body.choices?.length,1,'single completion');
  const choice=body.choices[0];
  assert.equal(choice.message?.role,'assistant');
  assert.equal(choice.finish_reason,'stop','natural stop, not output cap');
  assert.equal(choice.message?.content?.trim(),'42','functional arithmetic answer');
  const usage=body.usage,metrics=body.metrics;
  assert.ok(Number.isInteger(usage?.prompt_tokens)&&usage.prompt_tokens>0,'measured prompt usage');
  assert.ok(Number.isInteger(usage?.completion_tokens)&&usage.completion_tokens>0,'measured completion usage');
  assert.equal(usage.total_tokens,usage.prompt_tokens+usage.completion_tokens,'consistent total usage');
  assert.ok(metrics&&typeof metrics==='object','engine metrics returned');
  assert.ok(Number.isFinite(metrics.time_to_first_token_ms)&&metrics.time_to_first_token_ms>=0,'server TTFT available');
  assert.ok(Number.isFinite(metrics.generation_time_ms)&&metrics.generation_time_ms>=0,'server generation time available');
  return {
    prompt_tokens:usage.prompt_tokens,completion_tokens:usage.completion_tokens,total_tokens:usage.total_tokens,
    reasoning_tokens:usage.completion_tokens_details?.reasoning_tokens??null,
    server_ttft_ms:metrics.time_to_first_token_ms,
    client_ttft_ms:null,client_ttft_note:'Not measurable from a nonstreaming JSON response.',
    generation_time_ms:metrics.generation_time_ms,
    decode_tps:usage.completion_tokens>1&&metrics.generation_time_ms>0?(usage.completion_tokens-1)*1000/metrics.generation_time_ms:null,
    acceptance:metrics.speculative_decoding?.draft_acceptance_rate??null,
  };
}

function healthyIdle(response){
  return response.code===200&&response.body?.status==='ok'&&response.body.busy===false&&response.body.ranks?.length===2&&response.body.ranks.every(value=>value===true)&&!response.body.poison;
}

export async function main(argv=process.argv.slice(2)){
  const o=options(argv);
  if(fs.existsSync(o.output))throw Error('Output already exists. Reconcile its intent/result; a previous generation is never automatically replayed.');
  const token=fs.readFileSync(o.token_file,'utf8').trim();if(!token)throw Error('Empty local API token.');
  fs.mkdirSync(o.output,{recursive:false,mode:0o700});
  const redact=value=>String(value).split(token).join('[REDACTED]');
  function save(name,value){
    const dest=path.join(o.output,name),temporary=dest+'.tmp';
    fs.writeFileSync(temporary,redact(typeof value==='string'?value:JSON.stringify(value,null,2)),{mode:0o600});fs.renameSync(temporary,dest);
  }
  const report={started_utc:new Date().toISOString(),endpoint:o.endpoint,model:MODEL,phase:'PREFLIGHT',post_attempts:0};
  async function get(route){
    const response=await fetch(o.endpoint+route,{headers:{Authorization:'Bearer '+token},redirect:'error',credentials:'omit',signal:AbortSignal.timeout(10000)});
    const raw=await response.text();return {code:response.status,body:JSON.parse(raw)};
  }
  try{
    report.health_before=await get('/health');save('result.json',report);
    assert.ok(healthyIdle(report.health_before),'preflight: healthy, idle, both ranks ready');
    report.models=await get('/v1/models');save('result.json',report);
    assert.equal(report.models.code,200);assert.ok(report.models.body?.data?.some(model=>model.id===MODEL),'OpenAI model list contains pinned GLM');
    const request={model:MODEL,profile:'fast',messages:[{role:'user',content:'What is 6 times 7? Reply with the number only.'}],stream:false,temperature:0,seed:1,max_tokens:64,chat_template_kwargs:{reasoning_effort:'low'}};
    save('request.json',request);
    report.phase='SUBMIT_INTENT';save('intent.json',{created_utc:new Date().toISOString(),method:'POST',url:o.endpoint+'/v1/chat/completions',request_file:'request.json',retry_policy:'NEVER automatically resubmit; missing response is ambiguous.'});save('result.json',report);
    const started=performance.now();report.post_attempts=1;
    const response=await fetch(o.endpoint+'/v1/chat/completions',{method:'POST',headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'},body:JSON.stringify(request),redirect:'error',credentials:'omit',signal:AbortSignal.timeout(120000)});
    const raw=await response.text();report.http_seconds=(performance.now()-started)/1000;
    report.response_code=response.status;report.response_content_type=response.headers.get('content-type');report.phase='RESPONSE_RECEIVED';
    save('response.json',raw);save('timing.json',{http_seconds:report.http_seconds,client_ttft_ms:null,note:'Only full HTTP wall time is measured locally for nonstream JSON.'});save('result.json',report);
    assert.equal(response.status,200,'chat HTTP200');assert.match(report.response_content_type||'',/application\/json/,'nonstream JSON content type');
    const body=JSON.parse(raw);report.metrics=validateCompletion(body);report.answer=body.choices[0].message.content.trim();save('result.json',report);
    report.health_after=await get('/health');assert.ok(healthyIdle(report.health_after),'after completion: pair healthy and idle');
    report.status='PASS';report.phase='COMPLETE';report.finished_utc=new Date().toISOString();save('result.json',report);
    console.log(JSON.stringify({status:'PASS',post_attempts:1,answer:report.answer,server_ttft_ms:report.metrics.server_ttft_ms,http_seconds:report.http_seconds,raw:o.output},null,2));
    return report;
  }catch(error){
    report.status='FAIL';report.error=redact(String(error));report.finished_utc=new Date().toISOString();
    if(report.phase==='SUBMIT_INTENT')report.submission_outcome='UNKNOWN_AFTER_SUBMIT: do not automatically retry; the server may still be draining.';
    if(report.post_attempts===1&&!report.health_after){try{report.health_after=await get('/health');}catch(healthError){report.health_after_error=redact(String(healthError));}}
    save('result.json',report);throw error;
  }
}
if(process.argv[1]&&pathToFileURL(path.resolve(process.argv[1])).href===import.meta.url)main().catch(error=>{console.error(String(error));process.exitCode=1;});
