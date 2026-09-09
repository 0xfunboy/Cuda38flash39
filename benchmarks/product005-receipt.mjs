// Read-only product-deployment receipt. No model calls or lifecycle operations.
// Run from the repository root: node benchmarks/product005-receipt.mjs before|after [report-directory]
import assert from 'node:assert/strict';
import fs from 'node:fs';
import crypto from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';

const root = resolve('.');
const report = resolve(process.argv[3] || '/home/funboy/ai-exp/reports/moe-cluster/STRIX-PRODUCT-005');
const stage = process.argv[2];
assert.ok(['before', 'after'].includes(stage), 'Expected before or after');
const sha = p => crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex');
const properties = text => Object.fromEntries(text.trim().split('\n').map(line => { const n = line.indexOf('='); return [line.slice(0,n), line.slice(n+1)]; }));
const args = ['-p', 'Id', '-p', 'InvocationID', '-p', 'ActiveState', '-p', 'MainPID'];
const show = unit => properties(execFileSync('systemctl', ['--user', 'show', unit, ...args], { encoding: 'utf8' }));
const nodes = {
  gateway: show('strixglm.service'), rank0: show('ciru-model-rank0-001.service'), coordinator: show('ciru-frontend-001.service'),
  rank1: properties(execFileSync('ssh', ['-o', 'BatchMode=yes', '-o', 'ConnectTimeout=5', '02-evo-x3-tb', 'systemctl', '--user', 'show', 'ciru-model-rank1-001.service', ...args], { encoding: 'utf8' })),
};
const token = fs.readFileSync(root + '/state/api-token', 'utf8').trim();
const get = async p => {
  const response = await fetch('http://127.0.0.1:18093' + p, { headers: { Authorization: `Bearer ${token}` }, signal: AbortSignal.timeout(10000) });
  assert.equal(response.status, 200, `${p} must be ready`); return response.json();
};
const health = await get('/health'), sessions = await get('/v1/workspaces/sessions');
assert.equal(health.status, 'ok'); assert.ok(!health.busy && !health.poison); assert.deepEqual(health.ranks, [true, true]);
assert.ok(sessions.sessions.every(s => ['CLOSED', 'ERROR', 'INTERRUPTED'].includes(s.state)), 'Do not interrupt an open Pi workspace');
const receipt = {
  observed_utc: new Date().toISOString(), stage, nodes, health,
  binary_sha256: sha(root + '/bin/strixglm'), config_sha256: sha(root + '/config.json'),
  token_file_identity: ((s) => ({ inode: s.ino, size: s.size, mtime_ms: s.mtimeMs }))(fs.statSync(root + '/state/api-token')),
  whole_pair_snapshot_sha256: sha(root + '/state/legacy-handoff-20260908.json'),
  model_calls: 0,
};
if (stage === 'after') {
  const before = JSON.parse(fs.readFileSync(report + '/before.json', 'utf8'));
  for (const name of ['rank0', 'rank1', 'coordinator']) {
    assert.deepEqual(nodes[name], before.nodes[name], `${name} must be unchanged`);
    assert.equal(nodes[name].ActiveState, 'active');
  }
  for (const key of ['config_sha256', 'token_file_identity', 'whole_pair_snapshot_sha256']) assert.deepEqual(receipt[key], before[key], `${key} changed`);
  assert.equal(receipt.binary_sha256, sha(root + '/bin/strixglm.source-check'));
  receipt.settings = await get('/v1/settings');
  assert.deepEqual(receipt.settings.api, { chat: true, workspaces: true, legacy_coding: true, operations: true });
  receipt.options = await get('/v1/options'); assert.equal(receipt.options.tools_supported, true);
  const source = {};
  for (const rel of execFileSync('rg', ['--files'], { encoding: 'utf8' }).trim().split('\n')) {
    if (fs.statSync(rel).isFile()) source[rel] = sha(rel);
  }
  fs.writeFileSync(report + '/SOURCE-PIN.json', JSON.stringify({ observed_utc: receipt.observed_utc, base_commit: execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim(), binary_sha256: receipt.binary_sha256, files: source }, null, 2) + '\n', { mode: 0o600 });
}
fs.writeFileSync(report + '/' + stage + '.json', JSON.stringify(receipt, null, 2) + '\n', { mode: 0o600 });
console.log(JSON.stringify({ status: 'PASS', stage, both_ranks_healthy: true, idle: true, open_workspaces: 0, model_calls: 0 }));
