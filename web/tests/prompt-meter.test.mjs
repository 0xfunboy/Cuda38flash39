import test from 'node:test';
import assert from 'node:assert/strict';
import {promptMeasurement} from '../prompt-meter.mjs';

test('prefill wait is live but total prompt tokens are never treated as already processed',()=>{
  const timing={prompt_tokens:1000,dispatch_ms:100};
  const a=promptMeasurement({timing,elapsedMS:200}),b=promptMeasurement({timing,elapsedMS:500});
  assert.equal(a.rate,null);assert.equal(b.rate,null);
  assert.equal(a.phase,'waiting');assert.equal(b.waitMS,500);
});
test('observed prompt throughput and first-token time freeze independently of decode',()=>{
  const timing={prompt_tokens:1000,dispatch_ms:100,first_token_ms:600,preparation_ms:0,tokenize_ms:12};
  const a=promptMeasurement({timing,elapsedMS:600,firstTokenMS:630});
  const b=promptMeasurement({timing,elapsedMS:10000,firstTokenMS:630,metrics:{queue_time_ms:0,time_to_first_token_ms:400}});
  assert.equal(a.rate,2000);assert.equal(b.rate,a.rate);assert.equal(a.waitMS,600);assert.equal(b.waitMS,600);
  assert.equal(b.engine,400);assert.equal(b.queue,0);assert.equal(a.preparation,0);
  assert.equal(a.browser,false);
  const saved=promptMeasurement({metrics:{prompt_timing:timing,raw:{queue_time_ms:0,time_to_first_token_ms:400}},stopped:true});
  assert.equal(saved.rate,b.rate);assert.equal(saved.waitMS,b.waitMS);
});
test('missing, invalid and interrupted prefill measurements never produce a speed or completion',()=>{
  assert.equal(promptMeasurement({stopped:true,elapsedMS:200}).phase,'unavailable');
  assert.equal(promptMeasurement({metrics:{prompt_tokens:1000,server_ttft_ms:50},stopped:true}).rate,null);
  for(const timing of [{prompt_tokens:1000,dispatch_ms:100,first_token_ms:100},{prompt_tokens:-1,dispatch_ms:100,first_token_ms:200},{prompt_tokens:1000,dispatch_ms:Infinity,first_token_ms:200}])assert.equal(promptMeasurement({timing}).rate,null);
});
