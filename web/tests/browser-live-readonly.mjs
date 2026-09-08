// A read-only browser integration check against the real product gateway.
// Reads a local token file into process/browser memory. Never submits a model
// request, coding task, cancellation, apply operation or lifecycle command.
// Usage: node web/tests/browser-live-readonly.mjs URL TOKEN_FILE ARTIFACT_DIR
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createServer } from 'node:http';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const [baseArg, tokenArg, outputArg] = process.argv.slice(2);
if (!baseArg || !tokenArg || !outputArg) throw new Error('Usage: browser-live-readonly.mjs URL TOKEN_FILE ARTIFACT_DIR');
const apiURL = new URL(baseArg);
assert.equal(apiURL.protocol, 'http:');
assert.ok(['127.0.0.1', '[::1]', 'localhost'].includes(apiURL.hostname), 'Only loopback product URLs are permitted.');
assert.equal(apiURL.username + apiURL.password + apiURL.search + apiURL.hash, '');
const token = (await readFile(resolve(tokenArg), 'utf8')).trim();
assert.ok(token.length > 0, 'Local token is empty.');
const output = resolve(outputArg);
await mkdir(output, { recursive: true });
const redact = text => String(text).split(token).join('[REDACTED]');
const observations = { time_utc: new Date().toISOString(), api: apiURL.origin, readonly: true, model_requests: 0, checks: [] };
for (const endpoint of ['/health', '/v1/status', '/v1/models', '/ui-core.mjs']) {
  const response = await fetch(new URL(endpoint, apiURL), {
    headers: { Authorization: `Bearer ${token}` }, redirect: 'error', signal: AbortSignal.timeout(10000),
  });
  assert.equal(response.status, 200, `${endpoint} HTTP status`);
  const text = await response.text();
  const contentType = response.headers.get('content-type') || '';
  if (endpoint.endsWith('.mjs')) {
    assert.match(contentType, /javascript/);
    observations.javascript_mime = contentType;
    observations.current_ui_health_adapter = text.includes('function healthStatus');
  } else {
    observations[endpoint] = JSON.parse(redact(text));
  }
}
assert.equal(observations['/health'].status, 'ok');
assert.deepEqual(observations['/health'].ranks, [true, true]);
assert.ok(observations['/v1/models'].data.length >= 1);
assert.equal(observations['/v1/status'].nodes.length, 2);
for (const node of observations['/v1/status'].nodes) {
  assert.ok(node.memory_total_bytes > 0, `${node.name}: measured total memory`);
  assert.ok(node.memory_used_bytes > 0, `${node.name}: measured used memory`);
  assert.ok(node.gpu_utilization_percent >= 0 && node.gpu_utilization_percent <= 100, `${node.name}: measured GPU utilization`);
}
observations.checks.push('live health, both ranks, model, memory/GPU telemetry, JavaScript MIME');
const holder = createServer();
await new Promise(resolve => holder.listen(0, '127.0.0.1', resolve));
const port = holder.address().port;
await new Promise(resolve => holder.close(resolve));
const driver = spawn('geckodriver', ['--host', '127.0.0.1', '--port', String(port)], { stdio: ['ignore', 'pipe', 'pipe'] });
let log = '', session = '';
driver.stdout.on('data', data => { log += data; });
driver.stderr.on('data', data => { log += data; });
driver.on('error', error => { log += error.stack; });
const driverURL = `http://127.0.0.1:${port}`;
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
async function command(path, data) {
  const response = await fetch(driverURL + path, {
    method: data === undefined ? 'GET' : 'POST', headers: { 'Content-Type': 'application/json' },
    body: data === undefined ? undefined : JSON.stringify(data), signal: AbortSignal.timeout(45000),
  });
  const result = await response.json();
  if (!response.ok || result.value?.error) throw new Error(redact(JSON.stringify(result)));
  return result.value;
}
async function execute(script) { return command(`/session/${session}/execute/sync`, { script, args: [] }); }
async function until(expression) {
  for (let index = 0; index < 100; index++) {
    if (await execute(`return Boolean(${expression})`)) return;
    await delay(100);
  }
  throw new Error(`Browser condition not met: ${expression}`);
}
try {
  for (let index = 0; index < 100; index++) {
    try { await command('/status'); break; }
    catch (error) { if (index === 99) throw error; await delay(100); }
  }
  session = (await command('/session', { capabilities: { alwaysMatch: { browserName: 'firefox', 'moz:firefoxOptions': { args: ['-headless'] } } } })).sessionId;
  await command(`/session/${session}/window/rect`, { width: 1440, height: 1050 });
  await command(`/session/${session}/url`, { url: apiURL.href });
  await until('!document.getElementById("connection-panel").hidden');
  // Refuse browser-originated writes independently of the sequence below.
  await execute(`window.__readonlyRequests=[]; const nativeFetch=window.fetch.bind(window);window.fetch=(input,init={})=>{const method=(init.method||input.method||'GET').toUpperCase();const path=new URL(typeof input==='string'?input:input.url,location.href).pathname;window.__readonlyRequests.push({method,path});if(method!=='GET'&&method!=='HEAD')return Promise.reject(new Error('Readonly audit refuses write'));return nativeFetch(input,init);};document.getElementById('api-token').value=${JSON.stringify(token)};document.getElementById('connection-form').requestSubmit();`);
  await until('document.getElementById("connection-label").textContent === "Local API connected"');
  assert.equal(await execute('return document.getElementById("connection-panel").hidden'), true, 'Never screenshot the token input.');
  assert.equal(await execute('return document.getElementById("model-pill").textContent'), observations['/v1/status'].model);
  observations.checks.push('real browser auth, live model identity, hidden token field');
  await writeFile(resolve(output, 'chat-live-desktop.png'), Buffer.from(await command(`/session/${session}/screenshot`), 'base64'));
  await execute("document.getElementById('tab-cluster').click();");
  assert.equal(await execute('return document.getElementById("tab-cluster").getAttribute("aria-selected")'), 'true');
  await delay(220);
  assert.equal(await execute('return document.querySelectorAll(".node-title").length'), 2);
  const displayedHealth = await execute('return document.getElementById("cluster-health").textContent');
  assert.equal(displayedHealth, 'OK', 'Nested health.status must bind to the badge.');
  const activeLabel = await execute('return document.getElementById("cluster-active").textContent');
  assert.notEqual(activeLabel, '—', 'Live active request must not be unknown.');
  assert.equal(await execute('return document.getElementById("cluster-context").textContent.replaceAll(",", "")'), String(observations['/v1/status'].context_limit));
  const nodeTexts = await execute('return [...document.querySelectorAll(".node-detail strong")].map(n=>n.textContent)');
  assert.equal(nodeTexts.length, 4);
  assert.ok(nodeTexts.every(value => value !== '—'));
  observations.checks.push('cluster nested health, active request array, context, both node memory/GPU render');
  observations.browser_displayed = { health: displayedHealth, active_request: activeLabel, node_values: nodeTexts };
  await writeFile(resolve(output, 'cluster-live-desktop.png'), Buffer.from(await command(`/session/${session}/screenshot`), 'base64'));
  await command(`/session/${session}/window/rect`, { width: 390, height: 844 });
  for (const tab of ['chat', 'coding', 'cluster']) {
    await execute(`document.getElementById('tab-${tab}').click();`);
    assert.equal(await execute('return document.documentElement.scrollWidth <= window.innerWidth'), true, `${tab} mobile overflow`);
  }
  await delay(220);
  observations.checks.push('mobile chat, coding and cluster without horizontal overflow');
  await writeFile(resolve(output, 'cluster-live-mobile.png'), Buffer.from(await command(`/session/${session}/screenshot`), 'base64'));
  const requests = await execute('return window.__readonlyRequests');
  assert.ok(requests.every(row => ['GET', 'HEAD'].includes(row.method)));
  assert.equal(await execute('return localStorage.length + sessionStorage.length'), 0);
  observations.browser_requests = requests;
  observations.checks.push('browser GET-only and zero persisted secrets');
  observations.result = 'PASS';
  console.log(JSON.stringify({ result: 'PASS', checks: observations.checks.length, real_model_requests: 0, api: apiURL.origin, output }, null, 2));
} catch (error) {
  observations.result = 'FAIL';
  observations.error = redact(error.stack);
  throw error;
} finally {
  if (session) await fetch(`${driverURL}/session/${session}`, { method: 'DELETE', signal: AbortSignal.timeout(10000) }).catch(() => {});
  driver.kill('SIGTERM');
  await writeFile(resolve(output, 'live-readonly.json'), redact(JSON.stringify(observations, null, 2)));
  await writeFile(resolve(output, 'geckodriver.log'), redact(log));
}
