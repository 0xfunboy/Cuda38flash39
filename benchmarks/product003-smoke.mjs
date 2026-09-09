// Explicit, bounded integration probes; no imports run inference automatically.
// Never print or persist API credentials. Test artifacts are local/private.
import fs from 'node:fs';
import path from 'node:path';
const root='/home/funboy/StrixHaloClusterGLM';
const report='/home/funboy/ai-exp/reports/moe-cluster/STRIX-PRODUCT-003';
const mode=process.argv[2];
if(mode==='prepare'){
 if(fs.existsSync(report+'/preview-config.json'))throw Error('Preview receipt already exists; reuse it, do not reset the campaign.');
 const cfg=JSON.parse(fs.readFileSync(root+'/config.json','utf8'));
 cfg.listen='127.0.0.1:18096';cfg.state_dir=report+'/preview-state';cfg.tool_calls_supported=true;
 fs.mkdirSync(cfg.state_dir,{recursive:true,mode:0o700});
 fs.writeFileSync(report+'/preview-config.json',JSON.stringify(cfg,null,2)+'\n',{mode:0o600});
 console.log('Private preview configuration prepared; no generation.');process.exit(0);
}
const token=fs.readFileSync(report+'/preview-state/api-token','utf8').trim();
const base='http://127.0.0.1:18096';
async function api(route,payload){
 const start=performance.now();const res=await fetch(base+route,{method:payload?'POST':'GET',headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},body:payload?JSON.stringify(payload):undefined,signal:AbortSignal.timeout(650000)});
 const text=await res.text();let data;try{data=JSON.parse(text)}catch{data={raw:text}}
 return {status:res.status,seconds:(performance.now()-start)/1000,headers:Object.fromEntries([...res.headers].filter(([k])=>k.startsWith('x-strix'))),data};
}
if(mode==='tool'){
 if(fs.existsSync(report+'/tool-request.json'))throw Error('Tool probe already recorded; no automatic replay or overwrite.');
 const payload={model:'GLM5.3-Flash-CIRU-STRIX-IU4',messages:[{role:'user',content:'Use the read tool once to read README.md. Do not guess its contents. After the tool result, answer in one short sentence.'}],tools:[{type:'function',function:{name:'read',description:'Read a text file in the project.',parameters:{type:'object',properties:{path:{type:'string'}},required:['path'],additionalProperties:false}}}],reasoning_effort:'low',thinking_token_budget:128,max_tokens:512,temperature:0,seed:1};
 fs.writeFileSync(report+'/tool-request.json',JSON.stringify(payload,null,2));
 const first=await api('/v1/chat/completions',payload);fs.writeFileSync(report+'/tool-response.json',JSON.stringify(first,null,2));
 const msg=first.data.choices?.[0]?.message;const call=msg?.tool_calls?.[0];
 if(first.status!==200||first.data.choices?.[0]?.finish_reason!=='tool_calls'||msg.tool_calls.length!==1||call.function.name!=='read'||JSON.parse(call.function.arguments).path!=='README.md')throw Error('Tool protocol probe failed; inspect raw.');
 const follow={...payload,messages:[...payload.messages,msg,{role:'tool',tool_call_id:call.id,content:'The project is called PrimeCheck. It has a C11 prime-number implementation and sanitizer tests.'}]};
 fs.writeFileSync(report+'/tool-followup-request.json',JSON.stringify(follow,null,2));
 const second=await api('/v1/chat/completions',follow);fs.writeFileSync(report+'/tool-followup-response.json',JSON.stringify(second,null,2));
 if(second.status!==200||second.data.choices?.[0]?.finish_reason!=='stop'||!second.data.choices?.[0]?.message?.content?.includes('PrimeCheck'))throw Error('Tool followup failed; inspect raw.');
 console.log(JSON.stringify({first:{status:first.status,seconds:first.seconds,finish:first.data.choices[0].finish_reason},followup:{status:second.status,seconds:second.seconds,finish:second.data.choices[0].finish_reason,content:second.data.choices[0].message.content}},null,2));
}else if(mode==='pi'){
 if(fs.existsSync(report+'/pi-created.json'))throw Error('Pi run already recorded; inspect pi-status instead of duplicating the task.');
 const project=report+'/pi-project';fs.mkdirSync(project,{recursive:true,mode:0o700});
 fs.writeFileSync(project+'/README.md','# PrimeCheck\nWrite nth_prime.c with safe bounded input. Compile using gcc -std=c11 -Wall -Wextra -Werror -fsanitize=undefined,address.\n');
 const created=await api('/v1/workspaces/sessions',{kind:'local',root:project,reasoning_effort:'low',confirm:true});
 fs.writeFileSync(report+'/pi-created.json',JSON.stringify(created,null,2));if(created.status!==201)throw Error('Pi create failed');
 const sid=created.data.id;fs.writeFileSync(report+'/pi-session-id',sid+'\n');
 const started=await api(`/v1/workspaces/sessions/${sid}/start`,{confirm:true});fs.writeFileSync(report+'/pi-started.json',JSON.stringify(started,null,2));if(started.status!==200)throw Error('Pi start failed');
 const prompt='Read README.md. Then write nth_prime.c: a short C11 command-line program accepting exactly one decimal index N, 1..10000, printing only its Nth prime. Validate parsing/overflow; use division-safe primality checks. Compile with gcc -std=c11 -Wall -Wextra -Werror -fsanitize=undefined,address -o nth_prime nth_prime.c. Use bash to verify N=1 gives 2, 10 gives 29, 100 gives 541, and invalid abc/0 are rejected. Fix any compiler/test errors. Finish with a concise honest summary. Work only in this project. Do not install packages, access network, git or outside paths. Maximum 8 tool calls.';
 const sent=await api(`/v1/workspaces/sessions/${sid}/prompt`,{confirm:true,message:prompt});fs.writeFileSync(report+'/pi-prompt.json',JSON.stringify({prompt,response:sent},null,2));console.log(JSON.stringify({id:sid,status:sent.status,state:sent.data.state}));
}else if(mode==='pi-status'){
 const sid=fs.readFileSync(report+'/pi-session-id','utf8').trim();const result=await api(`/v1/workspaces/sessions/${sid}/events?after=0`);fs.writeFileSync(report+'/pi-events.json',JSON.stringify(result,null,2));
 const summary=result.data.events?.map(v=>{const e=v.event;return {seq:v.seq,type:e.type,tool:e.toolName,stop:e.message?.stopReason,error:e.error??e.errorMessage,text:e.message?.content?.filter(x=>x.type==='text').map(x=>x.text).join('\n')}}).filter(e=>['tool_execution_start','tool_execution_end','agent_end','message_end','response'].includes(e.type));
 console.log(JSON.stringify({state:result.data.state,events:summary},null,2));
}else if(mode==='pi-close'){
 const sid=fs.readFileSync(report+'/pi-session-id','utf8').trim();const result=await api(`/v1/workspaces/sessions/${sid}/close`,{confirm:true});fs.writeFileSync(report+'/pi-closed.json',JSON.stringify(result,null,2));console.log(result.status,result.data.state);
}else{throw Error('Modes: prepare | tool | pi | pi-status | pi-close')}
