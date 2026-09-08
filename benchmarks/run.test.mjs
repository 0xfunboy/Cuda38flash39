import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import http from 'node:http';
import {aggregate,median,effectiveReasoning,terminal,loadManifest,main} from './run.mjs';
test('median ignores missing telemetry, not zero',()=>{assert.equal(median([4,null,0,2,NaN]),2);assert.equal(median([2,4]),3);assert.equal(median([]),null);});
test('effective template reasoning does not invent medium support',()=>{assert.equal(effectiveReasoning('low'),'low');assert.equal(effectiveReasoning('high'),'high');assert.equal(effectiveReasoning('medium'),'max');});
test('solution rate includes failed task wall time and all repairs',()=>{
 const records=[{profile:'low',result:{status:'PASS',wall_seconds:20,model_calls:2,attempts:[{reasoning:'low',metrics:{completion_tokens:100,reasoning_tokens:10,final_tokens:90,prompt_tokens:500,decode_tps:25,ttft_ms:50}},{reasoning:'low',metrics:{completion_tokens:50,reasoning_tokens:5,final_tokens:45,prompt_tokens:600,decode_tps:20,ttft_ms:60}}]}},{profile:'low',result:{status:'INCOMPLETE',wall_seconds:40,model_calls:1,attempts:[{reasoning:'low',metrics:{completion_tokens:200,reasoning_tokens:200,final_tokens:0,prompt_tokens:500}}]}}];
 const [g]=aggregate(records);assert.equal(g.tasks,2);assert.equal(g.pass,1);assert.equal(g.first_pass,0);assert.equal(g.repaired_pass,1);assert.equal(g.repairs,1);assert.equal(g.model_calls,3);assert.equal(g.correct_solutions_per_hour,60);assert.equal(g.seconds_per_correct_including_failures,60);assert.equal(g.decode_tps_median,22.5);assert.equal(g.completion_tokens,350);assert.equal(g.reasoning_tokens,215);assert.equal(g.final_tokens,135);
});
test('blocked, cancelled and interrupted tasks are terminal; running is not',()=>{assert(terminal.has('BLOCKED'));assert(terminal.has('CANCELLED'));assert(terminal.has('INTERRUPTED'));assert(!terminal.has('running'));assert.deepEqual(aggregate([{profile:'low',result:{status:'running'}}]),[]);assert.equal(aggregate([{profile:'low',result:{status:'INTERRUPTED',model_calls:0,attempts:[]}}])[0].pass,0);});
test('no successes does not report a fictional solution time',()=>{const [g]=aggregate([{profile:'max',result:{status:'INCOMPLETE',wall_seconds:40,model_calls:1,attempts:[{reasoning:'medium',effective_reasoning:'max',metrics:{}}]}}]);assert.equal(g.seconds_per_correct_including_failures,null);assert.equal(g.correct_solutions_per_hour,0);assert.deepEqual(g.effective_reasoning,['max']);});
test('active manifest precedence replaces only duplicate ID',()=>{
 const temp=fs.mkdtempSync(path.join(os.tmpdir(),'strix-runner-manifest-'));
 try {for(const [name,value] of Object.entries({'a.json':{tasks:[{id:'x',value:1},{id:'y',value:2}]},'b.json':{tasks:[{id:'x',value:3}]},'all.json':{include_manifests:['a.json','b.json']}}))fs.writeFileSync(path.join(temp,name),JSON.stringify(value));assert.deepEqual(loadManifest(path.join(temp,'all.json')).tasks,[{id:'x',value:3},{id:'y',value:2}]);}
 finally{fs.rmSync(temp,{recursive:true,force:true});}
});
test('mock product submit, terminal receipt and resume never duplicate task',async()=>{
 const temp=fs.mkdtempSync(path.join(os.tmpdir(),'strix-runner-api-'));let posts=0,gets=0;
 const server=http.createServer((req,res)=>{assert.equal(req.headers.authorization,'Bearer test-token');res.setHeader('Content-Type','application/json');if(req.method==='POST'){posts++;req.resume();res.end(JSON.stringify({id:'mock-id',status:'queued'}));}else{gets++;res.end(JSON.stringify({id:'mock-id',status:'PASS',wall_seconds:5,model_calls:1,attempts:[{reasoning:'low',metrics:{completion_tokens:10}}]}));}});
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 try{
   fs.writeFileSync(path.join(temp,'token'),'test-token');fs.writeFileSync(path.join(temp,'task.json'),JSON.stringify({repo:'/fake',task:'mock only',files:['src.cpp']}));fs.writeFileSync(path.join(temp,'manifest.json'),JSON.stringify({tasks:[{id:'mock',spec:path.join(temp,'task.json')}]}));
   const argv=['--suite',path.join(temp,'manifest.json'),'--output',path.join(temp,'output'),'--endpoint',`http://127.0.0.1:${server.address().port}`,'--token-file',path.join(temp,'token')];await main(argv);await main(argv);assert.equal(posts,1);assert.equal(gets,1);assert.equal(JSON.parse(fs.readFileSync(path.join(temp,'output','summary.json'))).profiles[0].pass,1);
   const file=path.join(temp,'output','mock--low.receipt.json'),receipt=JSON.parse(fs.readFileSync(file));delete receipt.task_id;delete receipt.result;receipt.phase='SUBMIT_INTENT';fs.writeFileSync(file,JSON.stringify(receipt));await assert.rejects(()=>main(argv),/Ambiguous previous submission/);assert.equal(posts,1);
 }finally{await new Promise(r=>server.close(r));fs.rmSync(temp,{recursive:true,force:true});}
});
