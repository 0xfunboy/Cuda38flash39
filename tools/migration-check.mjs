// SYSTEM-CLEANUP-001: three bounded migration requests, not a speed campaign.
import fs from 'node:fs';
import assert from 'node:assert/strict';

const root='/home/funboy/StrixHaloClusterGLM';
const report=root+'/reports/SYSTEM-CLEANUP-001';
const phase=process.argv[2]??'migration-api';
assert.ok(['migration-api','final-migration-api'].includes(phase),'Unknown evidence phase');
const out=report+'/'+phase;
assert.ok(!fs.existsSync(out),'Existing evidence: do not replay requests');
fs.mkdirSync(out,{mode:0o700});
const token=fs.readFileSync(root+'/state/api-token','utf8').trim();
const save=(name,obj)=>fs.writeFileSync(out+'/'+name,JSON.stringify(obj,null,2)+'\n',{flag:'wx',mode:0o600});
const headers={Authorization:'Bearer '+token,'Content-Type':'application/json'};
async function get(route){
  const r=await fetch('http://127.0.0.1:18093'+route,{headers,signal:AbortSignal.timeout(10000)});
  assert.equal(r.status,200,route);return r.json();
}
async function post(id,base,route,body){
  save(id+'-intent.json',{base,route,body,retry:'none'});
  const r=await fetch(base+route,{method:'POST',headers:base.endsWith('18093')?headers:{'Content-Type':'application/json'},body:JSON.stringify(body),signal:AbortSignal.timeout(120000),redirect:'error'});
  const raw=await r.text();save(id+'-raw.json',{status:r.status,raw});assert.equal(r.status,200);
  return raw;
}
const health=await get('/health');
assert.equal(health.status,'ok');assert.deepEqual(health.ranks,[true,true]);assert.ok(!health.busy&&!health.poison);
save('health-before.json',health);
const options=await get('/v1/operations/options');save('operation-options.json',options);
for(const id of ['speed-historical','coding-sanity','context-small'])assert.equal(options.actions.find(x=>x.id===id)?.available,true,id);
const ref=JSON.parse(fs.readFileSync(report+'/target-reference/protocol.json','utf8'));
const rows=fs.readFileSync(report+'/target-reference/raw.jsonl','utf8').trim().split('\n').map(JSON.parse);
const expected=rows.find(x=>x.event==='request_end'&&x.phase==='measured').response.choices[0].token_ids.slice(0,96);
const raw=JSON.parse(await post('prefix','http://127.0.0.1:18094','/v1/completions',{...ref.request,max_tokens:96}));
assert.deepEqual(raw.choices[0].token_ids,expected,'Same target prefix after relocation');
const common={model:ref.request.model,temperature:0,seed:1,n:1,max_tokens:128,stream:true,stream_options:{include_usage:true}};
const tasks=[
 ['completion-stream','http://127.0.0.1:18094','/v1/completions',{...common,prompt:'[gMASK]<sop><|system|>Reasoning Effort: Low<|user|>Reply with the single word OK, then stop.<|assistant|><think>',echo:false,return_token_ids:true}],
 ['chat-stream','http://127.0.0.1:18093','/v1/chat/completions',{...common,profile:'low',messages:[{role:'user',content:'Reply with the single word OK, then stop.'}],chat_template_kwargs:{reasoning_effort:'low'}}],
];
const results=[{id:'prefix',status:'PASS',exact_target_tokens:96}];
for(const [id,base,route,body]of tasks){
 const text=await post(id,base,route,body);
 const data=text.split(/\r?\n/).filter(x=>x.startsWith('data:')).map(x=>x.slice(5).trim());
 assert.equal(data.at(-1),'[DONE]');
 const events=data.filter(x=>x!=='[DONE]').map(JSON.parse);
 const choices=events.flatMap(x=>x.choices||[]);
 assert.ok(choices.some(x=>x.finish_reason==='stop'));
 assert.ok(!choices.some(x=>x.finish_reason==='length'));
 const content=choices.map(x=>x.text??x.delta?.content??'').join('').replace(/^<\/think>/,'').trim();
 assert.equal(content,'OK');
 results.push({id,status:'PASS',natural_finish:true});
}
const after=await get('/health');assert.equal(after.status,'ok');assert.ok(!after.busy&&!after.poison);assert.deepEqual(after.ranks,[true,true]);
save('health-after.json',after);save('summary.json',{status:'PASS',requests:3,results,scope:'Relocation/API checks; no speed or intelligence claim'});
console.log(JSON.stringify({status:'PASS',requests:3,results}));
