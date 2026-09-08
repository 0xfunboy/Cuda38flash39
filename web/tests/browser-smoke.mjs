// Optional real-browser test, with a deterministic fake backend. No GLM calls,
// packages or browsers are downloaded. Requires installed Firefox/geckodriver.
// node web/tests/browser-smoke.mjs [artifact-directory]
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createServer } from 'node:http';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { resolve, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const web = fileURLToPath(new URL('../', import.meta.url));
const output = resolve(process.argv[2] || join(tmpdir(), `strixglm-web-${Date.now()}`));
await mkdir(output, { recursive: true });
// Keep the upload fixture beside the source: snap Firefox has a private /tmp.
const importPath = fileURLToPath(new URL('./fixtures/import-task.json', import.meta.url));
const apiToken = 'fixture-token-not-a-real-secret';
const calls = { chat: [], coding: [], apply: 0, cancel: 0, interrupted_status: 0 };
const forbidden = '<img src=x onerror="window.__xss=true">';
let cancelled = false;
let applied = false;
let applyingReads = 0;
const server = createServer(async (request, response) => {
  try {
    const url = new URL(request.url, 'http://127.0.0.1');
    const body = [];
    for await (const chunk of request) body.push(chunk);
    const payload = body.length ? JSON.parse(Buffer.concat(body).toString()) : {};
    function json(value, status = 200) {
      response.writeHead(status, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify(value));
    }
    if (url.pathname === '/health') return json({ ok: true });
    if (url.pathname.startsWith('/v1/') && request.headers.authorization !== `Bearer ${apiToken}`) return json({ error: 'Token required' }, 401);
    if (url.pathname === '/v1/models') return json({ data: [{ id: 'fixture-glm-not-a-live-model' }] });
    if (url.pathname === '/v1/status') return json({
      model: 'fixture-glm-not-a-live-model', health: 'healthy', context_limit: 8192,
      active_request: false, acceptance: 0.74,
      profiles: { fast: { reasoning: 'low', context_tokens: 4096 } },
      nodes: [{ name: 'NODE01', address: '10.55.0.1', health: 'healthy', memory_used_bytes: 45 * 1024 ** 3, memory_total_bytes: 128 * 1024 ** 3, gpu_utilization_percent: 32 }, { name: 'NODE02', address: '10.55.0.2', health: 'healthy' }],
    });
    if (url.pathname === '/v1/chat/completions') {
      calls.chat.push(payload);
      response.writeHead(200, { 'Content-Type': 'text/event-stream' });
      if (calls.chat.length === 2) return response.end(`data: ${JSON.stringify({ choices: [{ delta: { content: 'Truncated transport output' }, finish_reason: 'stop' }] })}\n\n`);
      const stream = `data: ${JSON.stringify({ choices: [{ delta: { reasoning_content: 'brief reasoning' } }] })}\r\n\r\ndata: ${JSON.stringify({ choices: [{ delta: { content: `Working code 🌱 ${forbidden}` } }] })}\n\ndata: ${JSON.stringify({ choices: [{ delta: {}, finish_reason: 'stop' }], usage: { prompt_tokens: 99, completion_tokens: 17 } })}\n\ndata: [DONE]\n\n`;
      const data = Buffer.from(stream);
      for (let index = 0; index < data.length; index += 3) response.write(data.subarray(index, index + 3));
      return response.end();
    }
    if (url.pathname === '/v1/coding/tasks' && request.method === 'POST') {
      calls.coding.push(payload);
      return json({ id: `fixture-${calls.coding.length}`, status: 'queued' }, 202);
    }
    if (/^\/v1\/coding\/tasks\/fixture-[12]\/apply$/.test(url.pathname)) {
      assert.equal(payload.confirm, true);
      calls.apply++;
      applied = true;
      applyingReads = 1;
      return json({ id: 'fixture-1', status: 'applying', applied: false });
    }
    if (url.pathname.endsWith('/fixture-2/cancel')) {
      calls.cancel++;
      cancelled = true;
      return json({ id: 'fixture-2', status: 'draining' });
    }
    if (url.pathname === '/v1/coding/tasks/fixture-2') return json({ id: 'fixture-2', status: cancelled ? 'cancelled' : 'running', profile: 'fast', attempts: [] });
    if (url.pathname === '/v1/coding/tasks/fixture-interrupted') {
      calls.interrupted_status++;
      return json({ id: 'fixture-interrupted', status: 'INTERRUPTED', profile: 'fast', error: 'process restarted; task is NOT automatically replayed', attempts: [] });
    }
    if (url.pathname === '/v1/coding/tasks/fixture-1') {
      const applying = applyingReads-- > 0;
      return json({
      id: 'fixture-1', status: applying ? 'applying' : 'PASS', profile: 'fast', applied: applying ? false : applied,
      files_changed: ['src/parser.c'], diff: `--- a/src/parser.c\n+++ b/src/parser.c\n@@ -1 +1 @@\n-buggy();\n+fixed(); // ${forbidden}\n`,
      attempts: [
        { index: 1, reasoning: 'low', status: 'FAIL', tests: { passed: false, output: 'Independent edge case failed', seconds: .12 }, metrics: { completion_tokens: 31 } },
        { index: 2, reasoning: 'low', status: 'PASS', build: { passed: true, output: 'compiled', seconds: .2 }, tests: { passed: true, output: '8 tests passed', seconds: .3 }, metrics: { completion_tokens: 33 } },
      ], wall_seconds: 4.23, model_calls: 2, metrics: { decode_tps: 24.8 }, final_response: 'Tested fixture patch ready.',
      });
    }
    const name = url.pathname === '/' ? 'index.html' : url.pathname.slice(1);
    if (!['index.html', 'styles.css', 'app.js', 'ui-core.mjs'].includes(name)) return json({ error: 'Not found' }, 404);
    response.writeHead(200, { 'Content-Type': name.endsWith('.css') ? 'text/css' : name.endsWith('.html') ? 'text/html' : 'text/javascript' });
    response.end(await readFile(join(web, name)));
  } catch (error) { response.writeHead(500); response.end(error.message); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const portHolder = createServer();
await new Promise(resolve => portHolder.listen(0, '127.0.0.1', resolve));
const driverPort = portHolder.address().port;
await new Promise(resolve => portHolder.close(resolve));
const driver = spawn('geckodriver', ['--host', '127.0.0.1', '--port', String(driverPort)], { stdio: ['ignore', 'pipe', 'pipe'] });
let driverLog = '';
driver.stdout.on('data', chunk => { driverLog += chunk; });
driver.stderr.on('data', chunk => { driverLog += chunk; });
driver.on('error', error => { driverLog += error.stack; });
let session = '';
const base = `http://127.0.0.1:${driverPort}`;
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
async function command(path, data) {
  const response = await fetch(`${base}${path}`, { method: data === undefined ? 'GET' : 'POST', headers: { 'Content-Type': 'application/json' }, body: data === undefined ? undefined : JSON.stringify(data), signal: AbortSignal.timeout(45000) });
  const result = await response.json();
  if (!response.ok || result.value?.error) throw new Error(JSON.stringify(result));
  return result.value;
}
async function execute(script) { return command(`/session/${session}/execute/sync`, { script, args: [] }); }
async function until(script) {
  for (let tries = 0; tries < 100; tries++) {
    if (await execute(`return Boolean(${script})`)) return;
    await delay(100);
  }
  throw new Error(`Browser condition timed out: ${script}`);
}
let tests = 0;
try {
  for (let tries = 0; tries < 100; tries++) {
    try { await command('/status'); break; }
    catch (error) { if (tries === 99) throw error; await delay(100); }
  }
  session = (await command('/session', { capabilities: { alwaysMatch: { browserName: 'firefox', 'moz:firefoxOptions': { args: ['-headless'] } } } })).sessionId;
  await command(`/session/${session}/window/rect`, { width: 1440, height: 1050 });
  await command(`/session/${session}/url`, { url: `http://127.0.0.1:${server.address().port}/` });
  await until('!document.getElementById("connection-panel").hidden');
  await execute(`document.getElementById('api-token').value=${JSON.stringify(apiToken)};document.getElementById('connection-form').requestSubmit();`);
  await until('document.getElementById("connection-label").textContent === "Local API connected"');
  tests++;
  await execute("document.getElementById('chat-input').value='Smoke test';document.getElementById('chat-form').requestSubmit();");
  await until('document.querySelector(".message.assistant .message-meta")?.textContent.includes("complete")');
  assert.equal(calls.chat.length, 1);
  assert.equal(calls.chat[0].stream, true);
  assert.equal(calls.chat[0].profile, 'fast');
  assert.equal(await execute('return document.querySelector(".message.assistant .message-content").textContent.includes("Working code 🌱")'), true);
  assert.equal(await execute('return !!window.__xss || !!document.querySelector(".conversation img")'), false);
  assert.equal(await execute('return document.getElementById("chat-context").textContent'), '99 / 8,192');
  await writeFile(join(output, 'chat-desktop.png'), Buffer.from(await command(`/session/${session}/screenshot`), 'base64'));
  tests++;
  await execute("document.getElementById('chat-input').value='Check truncated stream';document.getElementById('chat-form').requestSubmit();");
  await until('document.querySelectorAll(".message.assistant").length === 2 && !document.getElementById("send-chat").disabled');
  assert.equal(await execute('return [...document.querySelectorAll(".message.assistant .message-meta")].at(-1).textContent.endsWith("· error")'), true);
  assert.equal(await execute('return [...document.querySelectorAll(".message.assistant .message-error")].at(-1).textContent.includes("[DONE]")'), true);
  tests++;
  await execute("document.getElementById('tab-coding').click();");
  const upload = await command(`/session/${session}/element`, { using: 'css selector', value: '#code-spec' });
  await command(`/session/${session}/element/${upload['element-6066-11e4-a52e-4f735466cecf']}/value`, { text: importPath });
  await until('document.getElementById("import-summary").textContent.startsWith("Imported ")');
  assert.equal(calls.coding.length, 0, 'Import must never submit automatically.');
  assert.equal(await execute('return document.getElementById("code-repo").value'), '/fixture/repository');
  await execute("document.getElementById('coding-form').requestSubmit();");
  await until('document.getElementById("task-status").textContent === "PASS"');
  assert.equal(calls.coding.length, 1);
  assert.equal(calls.coding[0].apply, false);
  assert.equal(calls.coding[0].sandbox_policy, 'isolated');
  assert.deepEqual(calls.coding[0].allowed_paths, ['src/parser.c']);
  assert.deepEqual(calls.coding[0].test_files, { 'verify.c': '/fixture/independent/verify.c' });
  assert.equal(await execute('return document.querySelectorAll(".attempt").length'), 2);
  assert.equal(await execute('return !!window.__xss || !!document.querySelector(".diff img")'), false);
  assert.equal(await execute('return document.getElementById("apply-task").disabled'), false);
  tests++;
  await execute("setTimeout(()=>document.getElementById('apply-task').click(),0);");
  await delay(200);
  await command(`/session/${session}/alert/accept`, {});
  await until('document.getElementById("task-status").textContent === "APPLYING"');
  assert.equal(await execute('return document.getElementById("cancel-task").disabled'), true);
  assert.equal(await execute('return document.getElementById("start-task").disabled'), true);
  assert.equal(await execute('return document.getElementById("task-summary").textContent.includes("Waiting for the transaction")'), true);
  await until('document.getElementById("task-status").textContent === "PASS" && document.getElementById("apply-task").disabled');
  for (let tries = 0; calls.apply !== 1 && tries < 50; tries++) await delay(100);
  assert.equal(calls.apply, 1);
  tests++;
  await writeFile(join(output, 'coding-desktop.png'), Buffer.from(await command(`/session/${session}/screenshot`), 'base64'));
  await execute("document.getElementById('coding-form').requestSubmit();");
  await until('document.getElementById("task-status").textContent === "RUNNING"');
  await execute("document.getElementById('cancel-task').click();");
  await until('document.getElementById("task-status").textContent === "CANCELLED"');
  assert.equal(calls.cancel, 1);
  assert.equal(await execute('return document.getElementById("apply-task").disabled'), true);
  tests++;
  await execute("document.getElementById('resume-id').value='fixture-interrupted';document.getElementById('resume-form').requestSubmit();");
  await until('document.getElementById("task-status").textContent === "INTERRUPTED"');
  assert.equal(await execute('return document.getElementById("start-task").disabled'), false);
  assert.equal(await execute('return document.getElementById("cancel-task").disabled'), true);
  await delay(1800);
  assert.equal(calls.interrupted_status, 1, 'Interrupted work must stop automatic status polling.');
  tests++;
  await execute("document.getElementById('tab-cluster').click();");
  assert.equal(await execute('return document.querySelectorAll(".node-title").length'), 2);
  assert.equal(await execute('return document.getElementById("cluster-acceptance").textContent'), '74%');
  await writeFile(join(output, 'cluster-desktop.png'), Buffer.from(await command(`/session/${session}/screenshot`), 'base64'));
  tests++;
  await command(`/session/${session}/window/rect`, { width: 390, height: 844 });
  for (const tab of ['chat', 'coding', 'cluster']) {
    await execute(`document.getElementById('tab-${tab}').click();`);
    assert.equal(await execute('return document.documentElement.scrollWidth <= window.innerWidth'), true, `Mobile horizontal overflow: ${tab}`);
  }
  await writeFile(join(output, 'cluster-mobile.png'), Buffer.from(await command(`/session/${session}/screenshot`), 'base64'));
  tests++;
  await execute("document.getElementById('open-connection').click();document.getElementById('forget-token').click();");
  assert.equal(await execute('return document.getElementById("api-token").value'), '');
  assert.equal(await execute('return localStorage.length + sessionStorage.length'), 0);
  tests++;
  console.log(JSON.stringify({ result: 'PASS', browser: 'installed Firefox via WebDriver', checks: tests, backend: 'deterministic fixture, not live GLM', real_model_calls: 0, fixture_calls: { chat: calls.chat.length, coding: calls.coding.length, apply: calls.apply, cancel: calls.cancel }, output }, null, 2));
} finally {
  if (session) await fetch(`${base}/session/${session}`, { method: 'DELETE', signal: AbortSignal.timeout(10000) }).catch(() => {});
  driver.kill('SIGTERM');
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
  await writeFile(join(output, 'geckodriver.log'), driverLog);
}
