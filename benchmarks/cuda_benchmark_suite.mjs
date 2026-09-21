#!/usr/bin/env node
// Benchmark suite for Cuda38flash39 & Qwen 3.8 Flash Next
// Measures TTFT, prefill TPS, decode TPS, and latency across coding, repeated code, and prose.

import fs from 'node:fs';
import path from 'node:path';

const endpoint = process.env.ENDPOINT || 'http://127.0.0.1:18093';
const tokenFile = process.env.TOKEN_FILE || '/home/funboy/Cuda38flash39/state/api-token';

let token = '';
try {
  token = fs.readFileSync(tokenFile, 'utf8').trim();
} catch (e) {
  console.warn('Warning: token file not read, using empty auth');
}

const workloads = [
  {
    id: 'coding_novel',
    category: 'Code (Novel)',
    prompt: 'Implement an optimal thread-safe LRU Cache in modern C++20 using std::mutex, std::list, and std::unordered_map. Include template types Key and Value with get(), put(), and evict(). Provide clean comments and no unnecessary boilerplate.',
    max_tokens: 512,
  },
  {
    id: 'coding_repeated',
    category: 'Code (Repeated/Patterns)',
    prompt: 'Generate 15 repetitive CRUD REST API handler functions in Go for standard resource entities (User, Product, Order, Invoice, Customer, Item, Shipment, Payment, Subscription, Event, Log, Notification, Profile, Setting, Metric). Follow the exact same boilerplate structure for each.',
    max_tokens: 512,
  },
  {
    id: 'prose_reasoning',
    category: 'Prose & Reasoning',
    prompt: 'Analyze in detail the architectural trade-offs between Gated DeltaNet (linear recurrent state-space models) and standard Quadratic Self-Attention (QSA) in hybrid MoE architectures like Qwen 3.8 Flash Next. Discuss computational complexity, hardware memory bandwidth pressure on GPUs vs host RAM, and state compression decay over 128k token sequences.',
    max_tokens: 512,
  },
  {
    id: 'math_algorithmic',
    category: 'Algorithmic / Math',
    prompt: 'Solve this step by step: Find all integer solutions (x, y) to the Diophantine equation 7x + 13y = 2026. Then identify the solution where x and y are positive and |x - y| is minimized. Explain the Euclidean algorithm steps clearly.',
    max_tokens: 400,
  }
];

async function runBenchmark() {
  console.log('========================================================================');
  console.log(` Cuda38flash39 Benchmark Suite: Evaluation & Metric Collection`);
  console.log(` Target Endpoint: ${endpoint}`);
  console.log(` Date: ${new Date().toISOString()}`);
  console.log('========================================================================\n');

  const results = [];

  for (const wl of workloads) {
    process.stdout.write(`→ Testing ${wl.category} (${wl.id})... `);
    const startWall = performance.now();

    const requestBody = {
      messages: [{ role: 'user', content: wl.prompt }],
      max_tokens: wl.max_tokens,
      temperature: 0.1,
      stream: false,
    };

    const headers = { 'Content-Type': 'application/json' };
    if (token) headers['Authorization'] = `Bearer ${token}`;

    try {
      const resp = await fetch(`${endpoint}/v1/chat/completions`, {
        method: 'POST',
        headers,
        body: JSON.stringify(requestBody),
        signal: AbortSignal.timeout(120000),
      });

      const wallMs = performance.now() - startWall;

      if (!resp.ok) {
        const errText = await resp.text();
        console.log(`FAILED (HTTP ${resp.status}): ${errText.slice(0, 100)}`);
        continue;
      }

      const json = await resp.json();
      const usage = json.usage || {};
      const timings = json.timings || {};
      const metrics = json.metrics || {};

      const promptTokens = usage.prompt_tokens || 0;
      const completionTokens = usage.completion_tokens || 0;
      const totalTokens = usage.total_tokens || (promptTokens + completionTokens);

      let ttftMs = timings.prompt_ms || metrics.time_to_first_token_ms || null;
      let promptTps = timings.prompt_per_second || (ttftMs > 0 && promptTokens > 0 ? (promptTokens * 1000 / ttftMs) : null);
      let decodeTps = timings.predicted_per_second || (metrics.generation_time_ms > 0 && completionTokens > 1 ? ((completionTokens - 1) * 1000 / metrics.generation_time_ms) : null);

      if (!decodeTps && completionTokens > 0 && wallMs > 0) {
        const decodeWall = wallMs - (ttftMs || 0);
        if (decodeWall > 0) {
          decodeTps = (completionTokens * 1000) / decodeWall;
        }
      }

      const resObj = {
        id: wl.id,
        category: wl.category,
        promptTokens,
        completionTokens,
        totalTokens,
        wallSec: (wallMs / 1000).toFixed(2),
        ttftMs: ttftMs ? ttftMs.toFixed(1) : 'N/A',
        promptTps: promptTps ? promptTps.toFixed(1) : 'N/A',
        decodeTps: decodeTps ? decodeTps.toFixed(1) : 'N/A',
      };

      results.push(resObj);
      console.log(`DONE in ${resObj.wallSec}s | Prompt: ${promptTokens} t | Decode: ${completionTokens} t | Decode Speed: ${resObj.decodeTps} t/s`);
    } catch (err) {
      console.log(`ERROR: ${err.message}`);
    }
  }

  console.log('\n========================================================================');
  console.log(' BENCHMARK SUMMARY RESULTS TABLE');
  console.log('========================================================================');
  console.log('| Workload Category       | Prompt (t) | Gen (t) | TTFT (ms) | Prefill t/s | Decode t/s | Wall (s) |');
  console.log('|-------------------------|------------|---------|-----------|-------------|------------|----------|');
  for (const r of results) {
    const pad = (s, n) => String(s).padEnd(n, ' ');
    console.log(`| ${pad(r.category, 23)} | ${pad(r.promptTokens, 10)} | ${pad(r.completionTokens, 7)} | ${pad(r.ttftMs, 9)} | ${pad(r.promptTps, 11)} | ${pad(r.decodeTps, 10)} | ${pad(r.wallSec, 8)} |`);
  }
  console.log('========================================================================\n');

  return results;
}

runBenchmark().catch(console.error);
