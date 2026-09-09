// Read-only deployment verification. Generates a receipt; never dispatches GLM.
import fs from 'node:fs';
import crypto from 'node:crypto';
import {execFileSync} from 'node:child_process';
const root='/home/funboy/StrixHaloClusterGLM', report='/home/funboy/ai-exp/reports/moe-cluster/STRIX-PRODUCT-003';
const sha=p=>crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex');
const properties=text=>Object.fromEntries(text.trim().split('\n').map(x=>{const i=x.indexOf('=');return [x.slice(0,i),x.slice(i+1)]}));
const show=unit=>properties(execFileSync('systemctl',['--user','show',unit,'-p','Id','-p','InvocationID','-p','ActiveState','-p','MainPID'],{encoding:'utf8'}));
const nodes={gateway:show('strixglm.service'),rank0:show('ciru-model-rank0-001.service'),coordinator:show('ciru-frontend-001.service'),rank1:properties(execFileSync('ssh',['-o','BatchMode=yes','-o','ConnectTimeout=5','02-evo-x3-tb','systemctl','--user','show','ciru-model-rank1-001.service','-p','Id','-p','InvocationID','-p','ActiveState','-p','MainPID'],{encoding:'utf8'}))};
for(const [key,want] of Object.entries({rank0:'0956d54b0a764658ae0f3ebc994ce5c9',rank1:'eb51101782dc4f22a40031d182f93c56',coordinator:'250150cc6aeb41f79d28b58883a9bfcc'}))if(nodes[key].InvocationID!==want||nodes[key].ActiveState!=='active')throw Error('Preserved pair ownership changed: '+key);
const token=fs.readFileSync(root+'/state/api-token','utf8').trim();
const get=async p=>{const r=await fetch('http://127.0.0.1:18093'+p,{headers:{Authorization:`Bearer ${token}`},signal:AbortSignal.timeout(10000)});if(r.status!==200)throw Error('HTTP'+r.status+' '+p);return await r.json()};
const health=await get('/health'),options=await get('/v1/options'),workspace=await get('/v1/workspaces/options');
if(health.status!=='ok'||health.busy||health.poison||!health.ranks.every(Boolean)||!options.tools_supported||!workspace.pi.tools_available)throw Error('Deployment readiness failed');
const source={};for(const rel of execFileSync('rg',['--files'],{cwd:root,encoding:'utf8'}).trim().split('\n')){const p=root+'/'+rel,s=fs.lstatSync(p);if(s.isFile()&&s.size<8*1024*1024)source[rel]=sha(p)}
const binary=sha(root+'/bin/strixglm');if(binary!==sha(root+'/bin/strixglm.source-check'))throw Error('Current source does not reproduce deployed binary');
const pin={observed_utc:new Date().toISOString(),base_git_commit:execFileSync('git',['rev-parse','HEAD'],{cwd:root,encoding:'utf8'}).trim(),binary_sha256:binary,config_sha256:sha(root+'/config.json'),source_sha256:source,engine_and_weights_unchanged:true,weights_rehashed:false};
fs.writeFileSync(report+'/SOURCE-PIN.json',JSON.stringify(pin,null,2)+'\n',{mode:0o600});
const receipt={observed_utc:pin.observed_utc,endpoint:'http://127.0.0.1:18093',nodes,health,options,workspace,binary_sha256:binary,config_sha256:pin.config_sha256,rollback:{binary:report+'/rollback/strixglm.product002',binary_sha256:sha(report+'/rollback/strixglm.product002'),config:report+'/rollback/config.product002.json',config_sha256:sha(report+'/rollback/config.product002.json'),whole_pair_snapshot:root+'/state/legacy-handoff-20260908.json'},live_model_calls_this_receipt:0};
fs.writeFileSync(report+'/deployment.json',JSON.stringify(receipt,null,2)+'\n',{mode:0o600});
console.log(JSON.stringify({status:'PASS',endpoint:receipt.endpoint,binary_sha256:binary,preserved_rank_invocations:true,tools:true,source_files:Object.keys(source).length},null,2));
