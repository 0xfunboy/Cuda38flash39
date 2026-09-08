#!/usr/bin/env node
// Offline, reproducible preparation only: no model or network calls.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import crypto from 'node:crypto';
import {spawnSync} from 'node:child_process';
import {fixtures,harness} from './fixtures.mjs';
import {contextFixtures} from './context.mjs';
import {streamFixture} from './stream-fixture.mjs';
import {settingsFixture} from './settings-fixture.mjs';
import {coalescingFixture} from './coalescing-fixture.mjs';
import {utf8Fixture} from './utf8-fixture.mjs';
import {contextFixture} from './context.mjs';

const out=path.resolve(process.argv[2]||'/home/funboy/ai-exp/reports/moe-cluster/STRIX-PRODUCT-001/suite');
if(fs.existsSync(path.join(out,'manifest.json')))throw Error('Existing frozen manifest; use a new output directory.');
fs.mkdirSync(out,{recursive:true});
function write(file,text){fs.mkdirSync(path.dirname(file),{recursive:true});fs.writeFileSync(file,text,{flag:'wx'});}
function sha(text){return crypto.createHash('sha256').update(text).digest('hex');}
function execute(work,command){
  const argv=['--unshare-all','--die-with-parent','--new-session','--cap-drop','ALL','--clearenv','--setenv','PATH','/usr/bin:/bin','--setenv','LANG','C.UTF-8','--proc','/proc','--dev','/dev','--tmpfs','/tmp'];
  for(const p of ['/usr','/bin','/lib','/lib64'])if(fs.existsSync(p))argv.push('--ro-bind',p,p);
  argv.push('--bind',work,'/work','--chdir','/work','--remount-ro','/','/usr/bin/prlimit','--as=2147483648','--cpu=60','--fsize=33554432','--nofile=128','--core=0','--',...command);
  const started=performance.now();
  const p=spawnSync('/usr/bin/bwrap',argv,{encoding:'utf8',timeout:90000,maxBuffer:2**20});
  return {command,exit_code:p.status,signal:p.signal,error:p.error?.message||null,seconds:(performance.now()-started)/1000,stdout:p.stdout||'',stderr:p.stderr||''};
}
function validate(files,test,command){
  const work=fs.mkdtempSync(path.join(os.tmpdir(),'strix-fixture-'));
  try {
    for(const [name,text] of Object.entries(files))write(path.join(work,name),text);
    write(path.join(work,'verify.cpp'),harness+test);
    const build=execute(work,command);
    const result=build.exit_code===0?execute(work,['./verify-bin']):null;
    return {build,test:result};
  }finally{fs.rmSync(work,{recursive:true,force:true});}
}
const manifest={schema:1,created_utc:new Date().toISOString(),method:'Correct golden authored and compiled/tested before bug injection; buggy compiled and must fail same independent oracle. Neither golden nor hidden tests are prompt files.',tasks:[]};
const selected=process.argv.includes('--only')?new Set(process.argv[process.argv.indexOf('--only')+1].split(',')):null;
const additionalContext=process.argv.includes('--context-count')?[contextFixture(Number(process.argv[process.argv.indexOf('--context-count')+1]),'32k-expanded')]:[];
for(const fixture of [...fixtures,streamFixture,settingsFixture,coalescingFixture,utf8Fixture,...contextFixtures,...additionalContext].filter(x=>!selected||selected.has(x.id))) {
  const dir=path.join(out,fixture.id),repo=path.join(dir,'repo'),privateDir=path.join(dir,'private');
  const sources=Object.keys(fixture.files).filter(x=>x.endsWith('.cpp'));
  const build=['g++','-std=c++17','-O2','-Wall','-Wextra','-pedantic','-Iinclude',...sources,'verify.cpp','-o','verify-bin'];
  const golden=validate(fixture.files,fixture.test,build);
  if(golden.build.exit_code!==0||golden.test?.exit_code!==0) {
    fs.writeFileSync(path.join(out,'SETUP-FAIL.json'),JSON.stringify({id:fixture.id,golden},null,2));
    throw Error(`Golden failed: ${fixture.id}: ${JSON.stringify(golden)}`);
  }
  const buggyFiles={...fixture.files};
  const injections=fixture.injections||[fixture.injection];
  for(const [file,before,after] of injections){
    if(!buggyFiles[file].includes(before)||buggyFiles[file].split(before).length!==2)throw Error('Ambiguous injection '+fixture.id);
    buggyFiles[file]=buggyFiles[file].replace(before,after);
  }
  const buggy=validate(buggyFiles,fixture.test,build);
  if(buggy.build.exit_code!==0||buggy.test?.exit_code===0||buggy.test?.exit_code===null)throw Error(`Bug not discriminated ${fixture.id}: ${JSON.stringify(buggy)}`);
  const hashes={};
  for(const [name,text] of Object.entries(fixture.files)) {
    write(path.join(privateDir,'golden',name),text);
    write(path.join(repo,name),buggyFiles[name]);
    hashes[name]={golden:sha(text),buggy:sha(buggyFiles[name])};
  }
  write(path.join(privateDir,'verify.cpp'),harness+fixture.test);
  let patch='';
  for(const file of [...new Set(injections.map(x=>x[0]))]){
    const diff=spawnSync('diff',['-u','--label',`a/${file}`,'--label',`b/${file}`,path.join(repo,file),path.join(privateDir,'golden',file)],{encoding:'utf8'});
    if(diff.status!==1)throw Error('Missing/disallowed golden diff '+fixture.id+': '+diff.stderr);
    patch+=diff.stdout;
  }
  write(path.join(privateDir,'golden.patch'),patch);
  const spec={repo,task:fixture.task,files:Object.keys(fixture.files),allowed_paths:fixture.allowed_paths||Object.keys(fixture.files),build_command:build,test_command:['./verify-bin'],test_files:{'verify.cpp':path.join(privateDir,'verify.cpp')},profile:'fast',max_repairs:2,timeout:300,test_timeout:60,max_tokens:4096};
  write(path.join(dir,'task.json'),JSON.stringify(spec,null,2)+'\n');
  const receipt={id:fixture.id,category:fixture.category,language:'C++17',cases:fixture.cases,source_files:spec.files.length,context_characters:fixture.task.length+Object.values(buggyFiles).join('').length,nominal_context:fixture.nominal_context||null,context_caveat:fixture.note||null,hashes,test_sha256:sha(harness+fixture.test),golden_patch_sha256:sha(patch),golden,buggy,spec:path.join(dir,'task.json')};
  write(path.join(privateDir,'receipt.json'),JSON.stringify(receipt,null,2)+'\n');
  manifest.tasks.push(receipt);
  fs.writeFileSync(path.join(out,'manifest.in-progress.json'),JSON.stringify(manifest,null,2)+'\n');
  process.stdout.write(`${fixture.id}: golden PASS; buggy FAIL; ${spec.files.length} sources; ${receipt.context_characters} context chars\n`);
}
write(path.join(out,'manifest.json'),JSON.stringify(manifest,null,2)+'\n');
process.stdout.write(`Frozen ${manifest.tasks.length} tasks: ${path.join(out,'manifest.json')}\n`);
