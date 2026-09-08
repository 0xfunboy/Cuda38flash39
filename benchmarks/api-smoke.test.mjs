import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import http from 'node:http';
import {MODEL, main, options, validateCompletion} from './api-smoke.mjs';

const valid=()=>({object:'chat.completion',model:MODEL,choices:[{message:{role:'assistant',content:'42'},finish_reason:'stop'}],usage:{prompt_tokens:40,completion_tokens:2,total_tokens:42},metrics:{time_to_first_token_ms:100,generation_time_ms:50,speculative_decoding:{draft_acceptance_rate:0.5}}});

test('API smoke is opt-in, endpoint-pinned and does not mistake HTTP wall time for TTFT',()=>{
 assert.throws(()=>options(['--output','out']),/Explicit/);
 assert.throws(()=>options(['--output','out','--run-live','--endpoint','https://example.com']),/loopback/);
 const parsed=options(['--output','out','--run-live','--endpoint','http://127.0.0.1:18095/','--token-file','native/token']);assert.equal(parsed.endpoint,'http://127.0.0.1:18095');
 const metrics=validateCompletion(valid());assert.equal(metrics.server_ttft_ms,100);assert.equal(metrics.client_ttft_ms,null);assert.equal(metrics.decode_tps,20);
 const single=valid();single.usage={prompt_tokens:40,completion_tokens:1,total_tokens:41};single.metrics.generation_time_ms=0;assert.equal(validateCompletion(single).decode_tps,null);
});

test('API smoke rejects capped/wrong answers, missing engine metrics and inconsistent usage',()=>{
 for(const change of [body=>body.choices[0].finish_reason='length',body=>body.choices[0].message.content='43',body=>body.metrics={},body=>body.usage.total_tokens=123,body=>body.model='other']){
  const body=valid();change(body);assert.throws(()=>validateCompletion(body));
 }
});

async function withMock(lostResponse,run){
 const temp=fs.mkdtempSync(path.join(os.tmpdir(),'strix-json-smoke-')),tokenFile=path.join(temp,'token');fs.writeFileSync(tokenFile,'mock-smoke-secret');
 const calls=[];
 const server=http.createServer(async(req,res)=>{
  assert.equal(req.headers.authorization,'Bearer mock-smoke-secret');let raw='';for await(const chunk of req)raw+=chunk;
  const json=(body,status=200)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(body));};
  if(req.url==='/health')return json({status:'ok',busy:false,ranks:[true,true],poison:null});
  if(req.url==='/v1/models')return json({data:[{id:MODEL}]});
  if(req.method==='POST'&&req.url==='/v1/chat/completions'){
   calls.push(JSON.parse(raw));assert.ok(fs.existsSync(path.join(temp,'output','intent.json')),'intent must precede POST');
   if(lostResponse){res.destroy();return;}return json(valid());
  }
  return json({error:'unexpected route'},404);
 });
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 try{await run({temp,calls,args:['--endpoint',`http://127.0.0.1:${server.address().port}`,'--token-file',tokenFile,'--output',path.join(temp,'output'),'--run-live']});}
 finally{await new Promise(resolve=>server.close(resolve));fs.rmSync(temp,{recursive:true,force:true});}
}

test('mock API smoke sends exactly one deterministic JSON request and preserves redacted raw evidence',async()=>{
 await withMock(false,async({temp,calls,args})=>{
  const result=await main(args);assert.equal(result.status,'PASS');assert.equal(result.post_attempts,1);assert.equal(calls.length,1);
  assert.equal(calls[0].stream,false);assert.equal(calls[0].temperature,0);assert.equal(calls[0].seed,1);assert.equal(calls[0].max_tokens,64);assert.equal(calls[0].chat_template_kwargs.reasoning_effort,'low');
  assert.equal(JSON.parse(fs.readFileSync(path.join(temp,'output','response.json'),'utf8')).choices[0].message.content,'42');
  for(const file of fs.readdirSync(path.join(temp,'output')))assert.equal(fs.readFileSync(path.join(temp,'output',file),'utf8').includes('mock-smoke-secret'),false);
  await assert.rejects(()=>main(args),/never automatically replayed/);assert.equal(calls.length,1);
 });
});

test('mock lost response is ambiguous and is never retried',async()=>{
 await withMock(true,async({temp,calls,args})=>{
  await assert.rejects(()=>main(args));assert.equal(calls.length,1);
  const result=JSON.parse(fs.readFileSync(path.join(temp,'output','result.json'),'utf8'));
  assert.equal(result.status,'FAIL');assert.equal(result.phase,'SUBMIT_INTENT');assert.match(result.submission_outcome,/UNKNOWN_AFTER_SUBMIT/);assert.equal(result.post_attempts,1);
  await assert.rejects(()=>main(args),/never automatically replayed/);assert.equal(calls.length,1);
 });
});
