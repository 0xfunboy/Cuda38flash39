// Explicitly opt-in, real-model browser qualification. Never run concurrently
// with benchmarks. One short chat plus one isolated coding task, no apply.
// node web/tests/browser-live-actions.mjs URL TOKEN_FILE TASK_JSON ARTIFACT_DIR --run-live
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import { createServer } from 'node:http';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve, sep } from 'node:path';
import { SSEParser, completionDelta, importedTaskSpec } from '../ui-core.mjs';

const [baseArg, tokenArg, specArg, outputArg, consent] = process.argv.slice(2);
if (!baseArg || !tokenArg || !specArg || !outputArg || consent !== '--run-live') throw new Error('Explicit opt-in required: URL TOKEN_FILE TASK_JSON ARTIFACT_DIR --run-live');
const apiURL = new URL(baseArg);
assert.equal(apiURL.protocol, 'http:');
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(apiURL.hostname));
assert.equal(apiURL.username + apiURL.password + apiURL.search + apiURL.hash, '');
const token = (await readFile(resolve(tokenArg), 'utf8')).trim();
const specPath = resolve(specArg);
const originalSpec = JSON.parse(await readFile(specPath, 'utf8'));
const spec = importedTaskSpec(originalSpec);
assert.equal(spec.reasoning_effort, 'low', 'Use explicit low reasoning for this bounded browser qualification.');
const output = resolve(outputArg);
// A recorded submission must never be overwritten/replayed by rerunning this
// live acceptance command. Observe its saved task ID after an interruption.
await mkdir(output, { recursive: false, mode: 0o700 });
const redact = text => String(text).split(token).join('[REDACTED]');
const sha = text => createHash('sha256').update(text).digest('hex');
const originalHashes = {};
for (const path of spec.allowed_paths) {
  const full = resolve(spec.repo, path);
  assert.ok(full.startsWith(resolve(spec.repo) + sep), 'Editable paths must stay inside the fixture repository.');
  originalHashes[path] = sha(await readFile(full));
}
const report = { started_utc: new Date().toISOString(), api: apiURL.origin, task_spec: specPath, original_hashes: originalHashes, applied: false, model_requests: 0, checks: [] };
async function api(path) {
  const response = await fetch(new URL(path, apiURL), { headers: { Authorization: `Bearer ${token}` }, redirect: 'error', signal: AbortSignal.timeout(10000) });
  const value = await response.json();
  if (!response.ok) throw new Error(`Preflight/API failed: HTTP ${response.status}`);
  return value;
}
const before = await api('/v1/status');
assert.equal(before.health?.status, 'ok');
assert.equal(before.health?.busy, false, 'GLM is busy: do not run this qualification concurrently.');
assert.ok(Array.isArray(before.active_request) && before.active_request.length === 0, 'A coding task is already active.');
const options = await api('/v1/options');
assert.ok(options.reasoning_modes.includes('low'), 'This test requires the explicit low reasoning mode.');
report.preflight = before;

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
  const response = await fetch(driverURL + path, { method: data === undefined ? 'GET' : 'POST', headers: { 'Content-Type': 'application/json' }, body: data === undefined ? undefined : JSON.stringify(data), signal: AbortSignal.timeout(45000) });
  const result = await response.json();
  if (!response.ok || result.value?.error) throw new Error(redact(JSON.stringify(result)));
  return result.value;
}
async function execute(script) { return command(`/session/${session}/execute/sync`, { script, args: [] }); }
async function until(expression, milliseconds = 15000) {
  const deadline = Date.now() + milliseconds;
  while (Date.now() < deadline) {
    if (await execute(`return Boolean(${expression})`)) return;
    await delay(500);
  }
  throw new Error(`Browser condition timed out: ${expression}`);
}
async function screenshot(name) {
  assert.equal(await execute('return document.getElementById("panel-options").hidden'), true, 'Never screenshot the credential field.');
  await delay(200);
  await writeFile(resolve(output, name), Buffer.from(await command(`/session/${session}/screenshot`), 'base64'));
}

try {
  for (let index = 0; index < 100; index++) {
    try { await command('/status'); break; }
    catch (error) { if (index === 99) throw error; await delay(100); }
  }
  session = (await command('/session', { capabilities: { alwaysMatch: { browserName: 'firefox', 'moz:firefoxOptions': { args: ['-headless'] } } } })).sessionId;
  await command(`/session/${session}/window/rect`, { width: 1440, height: 1100 });
  await command(`/session/${session}/url`, { url: apiURL.href });
  await until('!document.getElementById("panel-options").hidden');
  await execute(`window.__liveAudit={requests:[],chatSSE:null};const nativeFetch=window.fetch.bind(window);window.fetch=async(input,init={})=>{const method=(init.method||input.method||'GET').toUpperCase();const path=new URL(typeof input==='string'?input:input.url,location.href).pathname;if(method==='POST'&&!['/v1/chat/completions','/v1/coding/tasks'].includes(path))throw new Error('Live qualification forbids apply, lifecycle or unrelated mutations');const body=init.body?JSON.parse(init.body):null;window.__liveAudit.requests.push({method,path,body});const response=await nativeFetch(input,init);if(method==='POST'&&path==='/v1/chat/completions'){window.__liveAudit.chatType=response.headers.get('content-type');response.clone().text().then(text=>window.__liveAudit.chatSSE=text);}return response;};document.getElementById('api-token').value=${JSON.stringify(token)};document.getElementById('connection-form').requestSubmit();`);
  await until('document.getElementById("connection-label").textContent === "Local API connected"');
  assert.equal(await execute('return !!document.getElementById("code-spec")'), true, 'Rebuild the Go embed assets to include task JSON import before this run.');
  await execute("document.getElementById('chat-input').value='Explain RAII in two concise sentences.';const cap=document.createElement('option');cap.value='128';cap.textContent='128 · bounded audit';document.getElementById('chat-cap').append(cap);document.getElementById('chat-cap').value='128';document.getElementById('reasoning-mode').value='low';document.getElementById('thinking-budget').value='';document.getElementById('chat-form').requestSubmit();");
  report.chat_submitted = true;
  report.model_requests = null;
  console.log('LIVE FRONTEND: one short fast/low chat submitted.');
  await until('document.querySelector(".message.assistant .message-meta") && !document.getElementById("send-chat").disabled && window.__liveAudit.chatSSE !== null', 120000);
  const chat = await execute('return {meta:document.querySelector(".message.assistant .message-meta").textContent,content:document.querySelector(".message.assistant .message-content").textContent,error:document.querySelector(".message.assistant .message-error").textContent,tps:document.getElementById("chat-tps").textContent,ttft:document.getElementById("chat-ttft").textContent,http:document.getElementById("chat-wall").textContent,tokens:document.getElementById("chat-tokens").textContent,context:document.getElementById("chat-context").textContent,type:window.__liveAudit.chatType,sse:window.__liveAudit.chatSSE}');
  report.chat = { ...chat, sse: undefined };
  await writeFile(resolve(output, 'chat.sse'), redact(chat.sse));
  assert.match(chat.type, /text\/event-stream/);
  assert.ok(chat.meta.includes('· complete'), `Chat was not naturally complete: ${chat.meta}; ${chat.error}`);
  assert.ok(chat.content.length > 20);
  assert.match(chat.content, /RAII|Resource Acquisition Is Initialization/i);
  let done = false, finish = '', content = '';
  const parser = new SSEParser(event => {
    if (event.data === '[DONE]') { done = true; return; }
    const delta = completionDelta(JSON.parse(event.data));
    content += delta.content;
    if (delta.finish) finish = delta.finish;
  });
  parser.push(chat.sse); parser.finish();
  assert.equal(done, true);
  assert.equal(finish, 'stop');
  assert.ok(content.trim().length > 0, 'Original SSE text must be present; Markdown rendering may omit formatting markers.');
  for (const field of ['tps', 'ttft', 'http', 'tokens', 'context']) assert.notEqual(chat[field], '—', `Actual chat ${field} must be collected.`);
  report.checks.push('real UI chat SSE, natural stop, visible final content and measured decode/TTFT/HTTP/tokens/context');
  report.model_requests = 1;
  await screenshot('chat-real-complete.png');

  await execute("document.getElementById('tab-coding').click();");
  const upload = await command(`/session/${session}/element`, { using: 'css selector', value: '#code-spec' });
  const elementID = upload['element-6066-11e4-a52e-4f735466cecf'];
  await command(`/session/${session}/element/${elementID}/value`, { text: specPath });
  await until('document.getElementById("import-summary").textContent.startsWith("Imported ")');
  assert.equal(await execute('return document.getElementById("code-repo").value'), spec.repo);
  await execute("document.getElementById('reasoning-mode').value='low';document.getElementById('coding-form').requestSubmit();");
  await until('document.getElementById("task-id").textContent !== "No task submitted" || !document.getElementById("coding-error").hidden', 30000);
  const submittedID = await execute('return document.getElementById("task-id").textContent');
  assert.match(submittedID, /^[a-zA-Z0-9._-]{8,160}$/);
  report.coding_task_id = submittedID;
  report.model_requests = null;
  await writeFile(resolve(output, 'submission.json'), JSON.stringify({ id: submittedID, task_spec: specPath, origin: 'real Firefox UI task JSON import and submit', apply: false }, null, 2));
  console.log(`LIVE FRONTEND: isolated coding task ${submittedID} submitted; waiting for functional result.`);
  const deadline = Math.min(7200000, spec.timeout * (spec.max_repairs + 1) * 1000 + 120000);
  await until('document.getElementById("task-status").textContent === "PASS" || ["FAIL","FAILED","INCOMPLETE","BLOCKED","CANCELLED","ERROR","INTERRUPTED"].includes(document.getElementById("task-status").textContent)', deadline);
  const result = await api(`/v1/coding/tasks/${submittedID}`);
  report.coding = result;
  report.model_requests = 1 + result.model_calls;
  assert.equal(result.status, 'PASS', `Coding did not pass: ${result.status}; ${result.error || ''}`);
  assert.ok(result.diff?.length > 0);
  assert.ok(result.files_changed?.length > 0);
  assert.equal(result.applied, false);
  assert.equal(result.attempts.at(-1).build.passed, true);
  assert.equal(result.attempts.at(-1).tests.passed, true);
  assert.equal(await execute('return document.getElementById("task-diff").textContent.includes("@@")'), true);
  assert.equal(await execute('return document.querySelectorAll(".attempt").length'), result.attempts.length);
  await execute("document.querySelectorAll('.attempt').forEach(node=>node.open=true);");
  assert.ok(await execute('return document.getElementById("attempts").textContent.includes("BUILD: PASS") && document.getElementById("attempts").textContent.includes("TESTS: PASS")'));
  await screenshot('coding-real-pass.png');
  report.checks.push('real task JSON import, isolated coding, final PASS, visible diff/build/tests/attempts');
  for (const [path, hash] of Object.entries(originalHashes)) assert.equal(sha(await readFile(resolve(spec.repo, path))), hash, `Original repository changed unexpectedly: ${path}`);
  report.checks.push('original source files unchanged; no automatic apply');
  const requests = await execute('return window.__liveAudit.requests');
  const writes = requests.filter(row => row.method === 'POST');
  assert.equal(writes.filter(row => row.path === '/v1/chat/completions').length, 1);
  assert.equal(writes.filter(row => row.path === '/v1/coding/tasks').length, 1);
  assert.equal(writes.find(row => row.path === '/v1/coding/tasks').body.apply, false);
  assert.deepEqual(writes.find(row => row.path === '/v1/coding/tasks').body.test_files, originalSpec.test_files);
  report.requests = requests;
  report.result = 'PASS';
  console.log(JSON.stringify({ result: 'PASS', checks: report.checks.length, model_requests: report.model_requests, coding_task_id: submittedID, output }, null, 2));
} catch (error) {
  report.result = 'FAIL';
  report.error = redact(error.stack);
  throw error;
} finally {
  report.finished_utc = new Date().toISOString();
  if (session) await fetch(`${driverURL}/session/${session}`, { method: 'DELETE', signal: AbortSignal.timeout(10000) }).catch(() => {});
  driver.kill('SIGTERM');
  await writeFile(resolve(output, 'frontend-e2e.json'), redact(JSON.stringify(report, null, 2)));
  await writeFile(resolve(output, 'geckodriver.log'), redact(log));
}
