// One bounded functional chat request. No retry, conversation creation, benchmark
// loop, model switch or service lifecycle operation.
import fs from 'node:fs';
import assert from 'node:assert/strict';
import {SSEParser, completionDelta} from '../web/ui-core.mjs';
import {promptMeasurement} from '../web/prompt-meter.mjs';
if(process.argv[2]!=='--run-live')throw Error('Explicit --run-live required');
const out=process.argv[3];if(!out || fs.existsSync(out))throw Error('New evidence directory required');
fs.mkdirSync(out,{mode:0o700});
const save=(name,value)=>fs.writeFileSync(out+'/'+name,JSON.stringify(value,null,2)+'\n',{flag:'wx',mode:0o600});
const token=fs.readFileSync(new URL('../state/api-token',import.meta.url),'utf8').trim();
const cfg=JSON.parse(fs.readFileSync(new URL('../config.json',import.meta.url)));
const payload={model:cfg.model,reasoning_effort:'low',context_tokens:4096,max_tokens:128,temperature:0,stream:true,stream_options:{include_usage:true,continuous_usage_stats:true},messages:[{role:'user',content:'Reply with the single word OK, then stop.'}]};
save('intent.json',{payload,requests:1,retry:false});
const started=performance.now();
const response=await fetch('http://127.0.0.1:18093/v1/chat/completions',{method:'POST',headers:{Authorization:'Bearer '+token,'Content-Type':'application/json','X-HaloClu-Timings':'1'},body:JSON.stringify(payload),signal:AbortSignal.timeout(120000)});
assert.equal(response.status,200);assert.match(response.headers.get('content-type'),/text\/event-stream/);
const events=[];let timing=null,first=null,done=false,text='',finish=null,usage=null,metrics=null;
const parser=new SSEParser(event=>{
  const at=performance.now()-started;
  events.push({at_ms:at,...event});
  if(event.data==='[DONE]'){done=true;return;}
  assert.notEqual(event.event,'error',event.data);
  const p=JSON.parse(event.data);
  if(event.event==='haloclu.timing'){timing=p;return;}
  const d=completionDelta(p);if((d.content||d.reasoning)&&first===null)first=at;
  text+=d.content;finish=d.finish||finish;usage=d.usage||usage;metrics=d.timings||metrics;
});
const decoder=new TextDecoder();
for await(const chunk of response.body)parser.push(decoder.decode(chunk,{stream:true}));
parser.push(decoder.decode());parser.finish();save('events.json',events);
assert.ok(done);assert.equal(finish,'stop');assert.equal(text.trim(),'OK');
const timingEvents=events.filter(e=>e.event==='haloclu.timing');
assert.equal(timingEvents.length,3);assert.ok(timingEvents[0].at_ms<first);
assert.equal(JSON.parse(timingEvents[0].data).first_token_ms,undefined);
assert.equal(timing.prompt_tokens,usage.prompt_tokens);
const measurement=promptMeasurement({timing,usage,metrics,stopped:true});
assert.ok(measurement.rate>0);assert.equal(measurement.phase,'received');
assert.equal(promptMeasurement({timing,usage,metrics,elapsedMS:999999,stopped:true}).rate,measurement.rate);
save('summary.json',{status:'PASS',requests:1,answer:text,first_timing_ms:timingEvents[0].at_ms,first_content_or_reasoning_ms:first,timing,measurement,scope:'Functional telemetry, not a performance qualification'});
console.log(JSON.stringify({status:'PASS',requests:1,answer:text,timing_events:timingEvents.length}));
