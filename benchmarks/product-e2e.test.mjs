import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import http from 'node:http';
import {disjointOutput, loopbackEndpoint, main, parseOptions, tree} from './product-e2e.mjs';

test('product E2E supports explicit native endpoint/token and GET-only observe without implicit execution',()=>{
 const parsed=parseOptions(['fixture.json','out','--run-live','--endpoint','http://127.0.0.1:18095/','--token-file','native/api-token']);
 assert.equal(parsed.endpoint,'http://127.0.0.1:18095/');assert.equal(parsed.token_file,'native/api-token');assert.equal(parsed.run_live,true);
 assert.equal(parseOptions(['out','--observe']).observe,true);
 assert.throws(()=>parseOptions(['fixture.json','out']),/Usage/);
 assert.throws(()=>parseOptions(['out','--observe','--run-live']),/Usage/);
 assert.equal(loopbackEndpoint('http://127.0.0.1:18095/'),'http://127.0.0.1:18095');
 for(const endpoint of ['https://example.com','http://user:secret@127.0.0.1:18095','http://127.0.0.1:18095/v1','http://127.0.0.1:18095/?token=secret'])assert.throws(()=>loopbackEndpoint(endpoint),/loopback/);
});

test('product E2E refuses recursive output overlap and fixture symlinks',()=>{
 const temp=fs.mkdtempSync(path.join(os.tmpdir(),'strix-product-scope-'));
 try{
  const repo=path.join(temp,'repo');fs.mkdirSync(repo);fs.writeFileSync(path.join(repo,'source.cpp'),'int main() {}');
  assert.throws(()=>disjointOutput(repo,path.join(repo,'nested-output')),/disjoint/);
  assert.doesNotThrow(()=>disjointOutput(repo,path.join(temp,'output')));
  assert.equal(Object.keys(tree(repo)).length,1);
  fs.symlinkSync('/etc/passwd',path.join(repo,'symlink'));
  assert.throws(()=>tree(repo),/symlink/);
 }finally{fs.rmSync(temp,{recursive:true,force:true});}
});

test('product E2E mock: manual apply and cancellation stay disposable, native settings resume GET-only',async()=>{
 const temp=fs.mkdtempSync(path.join(os.tmpdir(),'strix-product-api-'));
 const repo=path.join(temp,'repo'),output=path.join(temp,'output'),tokenPath=path.join(temp,'native-token');
 fs.mkdirSync(repo);fs.writeFileSync(path.join(repo,'source.cpp'),'buggy();\n');fs.writeFileSync(tokenPath,'fixture-secret');
 const specPath=path.join(temp,'task.json');fs.writeFileSync(specPath,JSON.stringify({repo,task:'Fix fixture',allowed_paths:['source.cpp'],files:['source.cpp'],test_command:['true'],build_command:['true'],apply:true}));
 const ids=['a'.repeat(32),'b'.repeat(32)],bodies=[];let cancelled=false,applied=false,mutations=0,applyingReads=1;
 const server=http.createServer(async(req,res)=>{
  let raw='';for await(const chunk of req)raw+=chunk;
  const body=raw?JSON.parse(raw):{};
  function json(value,status=200){res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(value));}
  if(req.url!=='/health'&&req.headers.authorization!=='Bearer fixture-secret')return json({error:'token required'},401);
  if(req.method==='POST')mutations++;
  if(req.url==='/health')return json({status:'ok',busy:bodies.length===2&&!cancelled});
  if(req.url==='/v1/status')return json({active_request:[],health:{status:'ok',busy:false}});
  if(req.url==='/v1/models')return json({data:[{id:'GLM5.3-Flash-CIRU-STRIX-IU4'}]});
  if(req.url==='/v1/chat/completions')return json({error:'negative cap'},400);
  if(req.url==='/v1/coding/tasks'&&req.method==='POST'){bodies.push(body);return json({id:ids[bodies.length-1],status:'queued'},202);}
  if(req.url===`/v1/coding/tasks/${ids[0]}`){
   if(applyingReads-- >0)return json({id:ids[0],status:'applying'});
   return json({id:ids[0],status:'PASS',applied,model_calls:1,files_changed:['source.cpp'],attempts:[{build:{passed:true,exit_code:0,timed_out:false},tests:{passed:true,exit_code:0,timed_out:false}}]});
  }
  if(req.url===`/v1/coding/tasks/${ids[0]}/apply`){
   if(!body.confirm)return json({error:'confirm required'},400);
   fs.writeFileSync(path.join(bodies[0].repo,'source.cpp'),'fixed();\n');applied=true;return json({id:ids[0],status:'PASS',applied:true});
  }
  if(req.url===`/v1/coding/tasks/${ids[1]}/cancel`){cancelled=true;return json({id:ids[1],status:'draining'},202);}
  if(req.url===`/v1/coding/tasks/${ids[1]}`)return json({id:ids[1],status:cancelled?'CANCELLED':'running',model_calls:1,applied:false});
  return json({error:'unexpected route'},404);
 });
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 try{
  const endpoint=`http://127.0.0.1:${server.address().port}`;
  const report=await main([specPath,output,'--run-live','--endpoint',endpoint,'--token-file',tokenPath]);
  assert.equal(report.status,'PASS');assert.equal(report.model_calls,2);assert.equal(bodies.length,2);
  for(const body of bodies){assert.equal(body.apply,false);assert.equal(body.sandbox_policy,'isolated');assert.notEqual(body.repo,repo);assert.ok(body.repo.startsWith(output+path.sep));}
  assert.equal(bodies[1].max_repairs,0);assert.notEqual(bodies[0].repo,bodies[1].repo);
  assert.equal(fs.readFileSync(path.join(repo,'source.cpp'),'utf8'),'buggy();\n');
  const saved=JSON.parse(fs.readFileSync(path.join(output,'result.json'),'utf8'));
  assert.equal(saved.endpoint,endpoint);assert.deepEqual(saved.tasks,ids);assert.equal(saved.apply_response.body.applied,true);
  const before=mutations;const observed=await main([output,'--observe']);
  assert.equal(observed.read_only,true);assert.equal(mutations,before);assert.equal(observed.tasks.length,2);
  await assert.rejects(()=>main([specPath,output,'--run-live','--endpoint',endpoint,'--token-file',tokenPath]),/Nothing is automatically resubmitted/);
  assert.equal(mutations,before);
  for(const name of fs.readdirSync(output).filter(name=>name.endsWith('.json')))assert.equal(fs.readFileSync(path.join(output,name),'utf8').includes('fixture-secret'),false);
 }finally{await new Promise(resolve=>server.close(resolve));fs.rmSync(temp,{recursive:true,force:true});}
});

test('lost submission response keeps intent evidence and never triggers automatic replay',async()=>{
 const temp=fs.mkdtempSync(path.join(os.tmpdir(),'strix-product-ambiguous-'));
 const repo=path.join(temp,'repo'),output=path.join(temp,'output'),tokenPath=path.join(temp,'token'),specPath=path.join(temp,'task.json');
 fs.mkdirSync(repo);fs.writeFileSync(path.join(repo,'a.cpp'),'original');fs.writeFileSync(tokenPath,'ambiguous-fixture-secret');
 fs.writeFileSync(specPath,JSON.stringify({repo,task:'Fix',allowed_paths:['a.cpp'],test_command:['true']}));
 let submissions=0;
 const server=http.createServer(async(req,res)=>{
  for await(const chunk of req)void chunk;
  const json=(value,status=200)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(value));};
  if(req.url!=='/health'&&!req.headers.authorization)return json({},401);
  if(req.url==='/health')return json({status:'ok',busy:false});
  if(req.url==='/v1/status')return json({active_request:[],health:{busy:false}});
  if(req.url==='/v1/models')return json({data:[{id:'GLM5.3-Flash-CIRU-STRIX-IU4'}]});
  if(req.url==='/v1/chat/completions')return json({},400);
  if(req.url==='/v1/coding/tasks'){submissions++;res.destroy();return;}
  return json({},404);
 });
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 try{
  const args=[specPath,output,'--run-live','--endpoint',`http://127.0.0.1:${server.address().port}`,'--token-file',tokenPath];
  await assert.rejects(()=>main(args));assert.equal(submissions,1);
  const saved=JSON.parse(fs.readFileSync(path.join(output,'result.json'),'utf8'));
  assert.equal(saved.status,'FAIL');assert.equal(saved.phase,'SUBMIT_APPLY_INTENT');assert.deepEqual(saved.tasks,[]);assert.ok(saved.resume_argv.includes('--observe'));
  const observed=await main([output,'--observe']);assert.equal(observed.manual_reconciliation_required,true);assert.match(observed.warning,/task ID was never received/);
  await assert.rejects(()=>main(args),/Nothing is automatically resubmitted/);assert.equal(submissions,1);
 }finally{await new Promise(resolve=>server.close(resolve));fs.rmSync(temp,{recursive:true,force:true});}
});
