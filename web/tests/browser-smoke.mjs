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
let apiToken = 'fixture-token-not-a-real-secret';
const calls = { chat: [], coding: [], apply: 0, cancel: 0, interrupted_status: 0, operation: [], attachments: 0, workspace: [], terminal: 0, shell: [], settings: [], rotations: 0 };
const conversations = new Map();
let conversationCounter = 0;
let handoffs = 0;
let piPrompts = 0;
let conversationDeletes = 0;
let workspaceEvents = [];
let deferConversationRead = false;
let releaseConversationRead = null;
function newConversation(title = 'Fixture conversation') {
  const id = (++conversationCounter).toString(16).padStart(32, '0');
  const c = { id, title, revision: 1, created: new Date().toISOString(), updated: new Date().toISOString(), messages: [], workspace_ids: [] };
  conversations.set(id, c); return c;
}
function conversationResponse(c) { return { ...c, replay_messages: c.messages.filter(m => m.role === 'user' || (m.role === 'assistant' && m.status === 'complete')).map(m => ({ role: m.role, content: m.content, ...(m.attachment_ids?.length ? { attachment_ids: m.attachment_ids } : {}) })) }; }
let api = { chat: true, workspaces: true, legacy_coding: true, operations: true };
let deferStatusFailure = false;
let releaseStatusFailure = null;
const forbidden = '<img src=x onerror="window.__xss=true">';
let cancelled = false;
let applied = false;
let applyingReads = 0;
let workspace = null;
const server = createServer(async (request, response) => {
  try {
    const url = new URL(request.url, 'http://127.0.0.1');
    const body = [];
    for await (const chunk of request) body.push(chunk);
    const multipart = (request.headers['content-type'] || '').startsWith('multipart/form-data');
    const payload = body.length && !multipart ? JSON.parse(Buffer.concat(body).toString()) : {};
    function json(value, status = 200) {
      response.writeHead(status, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify(value));
    }
    if (url.pathname === '/health') return json({ ok: true });
    if (url.pathname.startsWith('/v1/') && request.headers.authorization !== `Bearer ${apiToken}`) return json({ error: 'Token required' }, 401);
    if (url.pathname === '/v1/status' && deferStatusFailure) {
      deferStatusFailure = false;
      releaseStatusFailure = () => json({ error: 'Expired in-flight fixture credential' }, 401);
      return;
    }
    if (url.pathname === '/v1/settings/token') {
      assert.equal(request.method, 'POST'); assert.deepEqual(payload, { confirm: true });
      calls.rotations++; apiToken = 'rotated-fixture-not-a-real-secret'; return json({ token: apiToken });
    }
    if (url.pathname === '/v1/settings') {
      if (request.method === 'PUT') { calls.settings.push(payload); api = payload.api; }
      return json({ api, listen: '127.0.0.1:18093', backend: 'http://127.0.0.1:18091', model: 'fixture-glm-not-a-live-model', token_rotation_supported: true, api_note: 'Controls new requests only. Status, read, cancel and close remain available.', network_note: 'Managed by config; gateway restart required for network changes.' });
    }
    if (url.pathname === '/v1/models') return json({ data: [{ id: 'fixture-glm-not-a-live-model' }] });
    if (url.pathname === '/v1/conversations' && request.method === 'POST') return json(newConversation(payload.title), 201);
    if (url.pathname === '/v1/conversations') return json({ conversations: [...conversations.values()].map(c => ({ ...c, messages: undefined, message_count: c.messages.length })) });
    if (url.pathname.startsWith('/v1/conversations/')) {
      const [, , , id, action] = url.pathname.split('/');
      const c = conversations.get(id); if (!c) return json({ error: 'Unknown conversation' }, 404);
      if (request.method === 'GET') { if (deferConversationRead) { deferConversationRead = false; releaseConversationRead = () => json(conversationResponse(c)); return; } return json(conversationResponse(c)); }
      if (payload.revision !== c.revision) return json({ error: 'Revision conflict' }, 409);
      if (action === 'handoff') { assert.equal(payload.confirm, true); assert.equal(payload.workspace_id, workspace.id); handoffs++; return json({ executed: false, mode: 'transcript_context', bytes: 128 }); }
      if (request.method === 'PUT') { c.title = payload.title; c.messages = payload.messages; c.revision++; return json(conversationResponse(c)); }
      if (request.method === 'DELETE') { assert.equal(payload.confirm, true); if (workspace?.conversation_id === id && workspace.state !== 'CLOSED') return json({ error: 'Close linked workspace first', needs_close: true }, 409); if (workspace?.conversation_id === id) workspace = null; conversations.delete(id); conversationDeletes++; return json({ deleted: id }); }
    }
    if (url.pathname === '/v1/downloads/options') return json({ sources: ['huggingface', 'modelscope'], destination_root: '/fixture/downloads', concurrency: 1, disk_reserve_bytes: 17179869184 });
    if (url.pathname === '/v1/downloads/jobs') return json({ jobs: [] });
    if (url.pathname === '/v1/attachments' && request.method === 'POST') { assert.equal(multipart, true); calls.attachments++; return json({ id: 'attachment-fixture', name: 'fixture.json', kind: 'text', size_bytes: 128, sha256: 'fixture-hash-not-real', text: forbidden, extracted_bytes: 128, truncated: true, warning: 'Fixture extraction limit', created_utc: new Date().toISOString() }, 201); }
    if (url.pathname === '/v1/workspaces/options') return json({ local_roots: ['/fixture/repo'], pi: { installed: true, version: 'fixture', tools_available: true }, auth_methods: [{ id: 'key', available: true }], terminal: { commands: [{ id: 'pwd', label: 'Working directory' }] } });
    if (url.pathname === '/v1/workspaces/presets') return json({ presets: [] });
    if (url.pathname === '/v1/workspaces/sessions' && request.method === 'POST') { calls.workspace.push(payload); const c = conversations.get(payload.conversation_id) || newConversation(); c.workspace_ids.push('ws-fixture'); c.revision++; workspace = { id: 'ws-fixture', conversation_id: c.id, kind: 'local', root: '/fixture/protected-copy', original_root: '/fixture/repo', working_root: '/fixture/protected-copy', mode: payload.mode, reasoning_effort: payload.reasoning_effort, verification: { status: 'UNVERIFIED' }, state: 'CONNECTED', capabilities: { files: true, terminal: true, pi: true, prompt: false } }; return json(workspace, 201); }
    if (url.pathname === '/v1/workspaces/sessions') return json({ sessions: workspace ? [workspace] : [] });
    if (url.pathname === '/v1/workspaces/sessions/ws-fixture') return json(workspace);
    if (url.pathname.endsWith('/ws-fixture/start')) { workspace.state = 'READY'; workspace.capabilities.shell = true; workspace.capabilities.prompt = true; return json(workspace); }
    if (url.pathname.endsWith('/ws-fixture/close')) { workspace.state = 'CLOSED'; workspace.closed_cleanly = true; workspace.capabilities = {}; return json(workspace); }
    if (url.pathname.endsWith('/ws-fixture/events')) return json({ events: workspaceEvents.filter(event => event.seq > Number(url.searchParams.get('after') || 0)), next_seq: workspaceEvents.at(-1)?.seq || 0 });
    if (url.pathname.endsWith('/ws-fixture/prompt')) { assert.equal(payload.confirm, true); piPrompts++; const c = conversations.get(workspace.conversation_id); c.messages.push({ id: 'pi-user-fixture', role: 'user', origin: 'pi', content: payload.message, status: 'submitted', workspace_id: workspace.id }, { id: 'pi-assistant-fixture', role: 'assistant', origin: 'pi', content: 'Pi fixture parser review complete.', status: 'complete', workspace_id: workspace.id }); c.revision++; workspaceEvents = [{ seq: 1, event: { type: 'message_end', message: { role: 'assistant', content: [{ type: 'text', text: 'Pi fixture parser review complete.' }] } } }, { seq: 2, event: { type: 'agent_end' } }]; return json(workspace); }
    if (url.pathname.endsWith('/ws-fixture/files')) return json({ path: '', entries: [{ name: 'main.c', path: 'main.c', type: 'file', size: 31 }] });
    if (url.pathname.endsWith('/ws-fixture/file')) return json({ path: 'main.c', content: forbidden, truncated: false });
    if (url.pathname.endsWith('/ws-fixture/terminal')) {
      if (payload.command) { assert.equal(workspace.state, 'READY'); calls.shell.push(payload); return json({ output: 'custom fixture output', exit_code: 0, seconds: .01 }); }
      calls.terminal++; assert.deepEqual(payload, { command_id: 'pwd', confirm: true }); return json({ output: '/fixture/repo', exitcode: 0, seconds: .01 });
    }
    if (url.pathname === '/v1/options') return json({ reasoning_modes: ['low', 'high', 'max'], context_options: [4096, 8192, 16384, 32768, 65536], default_reasoning: 'low', default_context_tokens: 65536, default_max_tokens: 16384, max_output_tokens: 32768, engine_context_tokens: 65536, generation_timeout_seconds: 600, thinking_budget_supported: true, quality_note: 'Fixture settings are not real quality qualification.' });
    if (url.pathname === '/v1/catalog') return json({ schema: 1, models: [{ id: 'fixture-glm-not-a-live-model', name: 'GLM fixture', status: 'READY', format: 'W4', runtime: 'fixture', distribution: 'TP2', quality_limits: ['Not a live benchmark'], assets: [{ id: 'fixture-asset', path: '/fixture/model', present: null, verification: 'not_checked' }], sources: [{ url: 'javascript:alert(1)', revision: '<script>unsafe</script>' }], actions: { load: { enabled: false, reason: 'Already active' }, download: { enabled: false, reason: 'No missing asset' } }, evidence: [{ label: 'Historical fixture only', status: 'PASS', decode_tps: 24.8, http_tps: 23.1, context_tokens: 8192, report: '/fixture/raw', notes: 'Not comparable to other formats' }] }] });
    if (url.pathname === '/v1/operations/options') return json({ actions: [{ id: 'api-smoke', label: 'API smoke', description: 'One fixture request', available: true, requires_confirmation: true, requests: 1 }, { id: 'context-long', label: 'Long context', available: false, blocked_reason: 'Fixture disabled' }] });
    if (url.pathname === '/v1/operations/jobs' && request.method === 'POST') {
      if (!api.operations) return json({ error: 'Operation new requests are paused in Options', code: 'api_disabled' }, 403);
      calls.operation.push(payload); return json({ id: 'op-1', action_id: payload.action_id, status: 'queued' }, 202);
    }
    if (url.pathname === '/v1/operations/jobs') return json({ jobs: calls.operation.length ? [{ id: 'op-1', action_id: 'api-smoke', status: 'PASS', result: { fixture: true }, raw_directory: '/fixture/raw' }] : [] });
    if (url.pathname === '/v1/status') return json({
      model: 'fixture-glm-not-a-live-model', health: 'healthy', context_limit: 8192,
      active_request: false, acceptance: 0.74,
      profiles: { fast: { reasoning: 'low', context_tokens: 4096 } },
      nodes: [{ name: 'NODE01', address: '10.55.0.1', health: 'healthy', memory_used_bytes: 45 * 1024 ** 3, memory_total_bytes: 128 * 1024 ** 3, gpu_utilization_percent: 32 }, { name: 'NODE02', address: '10.55.0.2', health: 'healthy' }],
    });
    if (url.pathname === '/v1/chat/completions') {
      calls.chat.push(payload);
      const c = conversations.get(payload.conversation_id); assert.ok(c, 'Chat must use a server-generated conversation ID.');
      c.messages.push({ ...payload.messages.at(-1), id: `user-${calls.chat.length}`, origin: 'chat', status: 'submitted', settings: { reasoning_effort: payload.reasoning_effort } });
      c.messages.push({ id: `assistant-${calls.chat.length}`, origin: 'chat', role: 'assistant', content: calls.chat.length === 2 ? 'Truncated transport output' : `Working code 🌱 ${forbidden}\n\n\`\`\`html\n${forbidden}\n\`\`\`\n`, reasoning: 'brief reasoning', status: calls.chat.length === 2 ? 'error' : 'complete', settings: { reasoning_effort: payload.reasoning_effort, metrics: { prompt_tokens: 99, completion_tokens: 17, http_seconds: .8, ttft_ms: 30, decode_tps: null } } }); c.revision++;
      response.writeHead(200, { 'Content-Type': 'text/event-stream', 'X-StrixGLM-Context-Tokens': '65536', 'X-StrixGLM-Prompt-Tokens': '99', 'X-StrixGLM-Max-Tokens': '16384' });
      if (calls.chat.length === 2) return response.end(`data: ${JSON.stringify({ choices: [{ delta: { content: 'Truncated transport output' }, finish_reason: 'stop' }] })}\n\n`);
      const stream = `data: ${JSON.stringify({ choices: [{ delta: { reasoning_content: 'brief reasoning' } }], usage: { prompt_tokens: 99, completion_tokens: 2 } })}\r\n\r\ndata: ${JSON.stringify({ choices: [{ delta: { content: `Working code 🌱 ${forbidden}\n\n\`\`\`html\n${forbidden}\n\`\`\`\n` } }], usage: { prompt_tokens: 99, completion_tokens: 15 } })}\n\ndata: ${JSON.stringify({ choices: [{ delta: {}, finish_reason: 'stop' }], usage: { prompt_tokens: 99, completion_tokens: 17 } })}\n\ndata: [DONE]\n\n`;
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
    if (!['index.html', 'styles.css', 'app.js', 'ui-core.mjs', 'downloads.mjs', 'assets/haloclu-icon.png', 'assets/haloclu-horizontal.png'].includes(name)) return json({ error: 'Not found' }, 404);
    response.writeHead(200, { 'Content-Type': name.endsWith('.png') ? 'image/png' : name.endsWith('.css') ? 'text/css' : name.endsWith('.html') ? 'text/html' : 'text/javascript' });
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
  await until('!document.getElementById("panel-options").hidden');
  await execute(`document.getElementById('api-token').value=${JSON.stringify(apiToken)};document.getElementById('connection-form').requestSubmit();`);
  await until('document.getElementById("connection-label").textContent === "Local API connected"');
  assert.equal(await execute('return document.getElementById("tab-coding").hidden'), true);
  assert.equal(await execute('return document.querySelectorAll("[data-tab]:not([hidden])").length'), 6);
  await until('[...document.querySelectorAll(".brand img")].every(img=>img.complete&&img.naturalWidth>0)');
  await execute("document.getElementById('tab-cluster').focus();document.getElementById('tab-cluster').dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowDown',bubbles:true}));");
  assert.equal(await execute('return document.activeElement.id'), 'tab-options', 'Keyboard must skip hidden legacy tab.');
  for (const tab of ['chat', 'workspace', 'models', 'benchmarks', 'cluster', 'options']) {
    await execute(`document.getElementById('tab-${tab}').click();`);
    assert.equal(await execute('return document.getElementById("generation-settings").hidden'), !['chat', 'workspace'].includes(tab), `${tab}: Generation scope`);
    if (tab === 'workspace') {
      assert.equal(await execute('return document.getElementById("generation-chat-controls").hidden'), true);
      assert.equal(await execute('return document.getElementById("generation-workspace-note").hidden'), false);
    }
  }
  await execute("document.getElementById('ui-show-advanced').checked=true;document.getElementById('ui-show-advanced').dispatchEvent(new Event('change'));document.getElementById('tab-coding').click();");
  assert.equal(await execute('return document.getElementById("panel-coding").hidden'), false);
  assert.equal(await execute('return document.getElementById("generation-settings").hidden'), false);
  await execute("document.getElementById('ui-show-advanced').checked=false;document.getElementById('ui-show-advanced').dispatchEvent(new Event('change'));");
  assert.equal(await execute('return document.getElementById("panel-chat").hidden'), false, 'Hiding selected legacy falls back to Chat.');
  assert.equal(await execute('return document.getElementById("tab-coding").getClientRects().length'), 0);
  tests++;
  assert.equal(await execute('return document.documentElement.lang'), 'en');
  assert.equal(await execute('return document.getElementById("code-timeout").max'), '600');
  tests++;
  const attachmentUpload = await command(`/session/${session}/element`, { using: 'css selector', value: '#chat-attachments' });
  await command(`/session/${session}/element/${attachmentUpload['element-6066-11e4-a52e-4f735466cecf']}/value`, { text: importPath });
  await until('document.querySelectorAll(".attachment").length === 1');
  assert.equal(calls.chat.length, 0, 'Uploading must not run inference.');
  assert.equal(await execute('return document.getElementById("attachment-list").textContent.includes("Extraction truncated")'), true);
  assert.equal(await execute('return !!document.querySelector(".attachment img")'), false);
  tests++;
  await execute("document.getElementById('chat-input').value='Smoke test';document.getElementById('chat-form').requestSubmit();document.getElementById('chat-form').requestSubmit();");
  await until('document.querySelector(".message.assistant .message-meta")?.textContent.includes("complete")');
  assert.equal(calls.chat.length, 1);
  assert.equal(calls.chat[0].stream, true);
  assert.equal(calls.chat[0].profile, undefined);
  assert.equal(calls.chat[0].reasoning_effort, 'low');
  assert.equal(calls.chat[0].context_tokens, 65536);
  assert.equal(calls.chat[0].max_tokens, undefined);
  assert.equal(calls.chat[0].thinking_token_budget, undefined);
  assert.equal(calls.chat[0].stream_options.continuous_usage_stats, true);
  assert.deepEqual(calls.chat[0].messages[0].attachment_ids, ['attachment-fixture']);
  assert.equal(calls.chat[0].messages[0].content, 'Smoke test', 'Extracted attachment text must not be duplicated client-side.');
  assert.equal(await execute('return document.querySelector(".message.assistant .message-content").textContent.includes("Working code 🌱")'), true);
  assert.equal(await execute('return !!window.__xss || !!document.querySelector(".conversation img")'), false);
  assert.equal(await execute('return document.getElementById("chat-context").textContent'), '99');
  assert.equal(await execute('return document.getElementById("chat-live-tps").textContent'), '—', 'Saved history must not pretend to be a live stream.');
  assert.equal(await execute('return document.getElementById("chat-ttft").title.includes("Saved gateway-observed")'), true);
  assert.equal(await execute('return document.getElementById("chat-tps").textContent'), '—');
  assert.equal(await execute('return document.querySelectorAll(".message .code-block").length'), 1);
  assert.equal(await execute('return document.querySelector(".message .code-block").textContent.includes("```")'), false);
  await execute("window.__exported=null;const originalCreateURL=URL.createObjectURL;URL.createObjectURL=blob=>{if(blob.type.startsWith('application/json'))blob.text().then(text=>window.__exported=JSON.parse(text));return originalCreateURL(blob)};document.getElementById('export-chat').click();");
  await until('window.__exported !== null');
  const exported = await execute('return window.__exported');
  assert.deepEqual(exported.messages[0].attachment_ids, ['attachment-fixture']);
  assert.equal(JSON.stringify(exported).includes(apiToken), false);
  assert.equal(exported.messages[1].status, 'complete');
  await writeFile(join(output, 'chat-desktop.png'), Buffer.from(await command(`/session/${session}/screenshot`), 'base64'));
  tests++;
  await execute("document.getElementById('reasoning-mode').value='high';document.getElementById('thinking-budget').value='128';document.getElementById('context-select').value='8192';document.getElementById('chat-cap').value='4096';");
  await execute("document.getElementById('chat-input').value='Check truncated stream';document.getElementById('chat-form').requestSubmit();");
  await until('document.querySelectorAll(".message.assistant").length === 2 && !document.getElementById("send-chat").disabled');
  assert.equal(await execute('return [...document.querySelectorAll(".message.assistant .message-meta")].at(-1).textContent.includes("· error")'), true);
  assert.equal(await execute('return [...document.querySelectorAll(".message.assistant .message-error")].at(-1).textContent.includes("not replayed")'), true);
  assert.equal(calls.chat[1].reasoning_effort, 'high');
  assert.equal(calls.chat[1].thinking_token_budget, 128);
  assert.equal(calls.chat[1].max_tokens, 4096);
  assert.equal(calls.chat[1].context_tokens, 8192);
  tests++;
  await execute("document.getElementById('tab-options').click();document.getElementById('ui-show-advanced').checked=true;document.getElementById('ui-show-advanced').dispatchEvent(new Event('change'));document.getElementById('tab-coding').click();");
  assert.equal(await execute('return document.getElementById("context-select").value'), '8192', 'Tab switches preserve settings.');
  assert.equal(await execute('return document.getElementById("chat-cap").value'), '4096');
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
  await execute("document.getElementById('tab-models').click();");
  await until('document.querySelectorAll(".catalog-card").length === 1');
  assert.equal(await execute('return document.querySelector(".catalog-card").textContent.includes("Presence not checked")'), true);
  assert.equal(await execute('return !!document.querySelector(".catalog-card a[href^=javascript]") || !!window.__xss'), false);
  assert.equal(calls.operation.length, 0, 'Opening catalog must never start operations.');
  await writeFile(join(output, 'models-desktop.png'), Buffer.from(await command(`/session/${session}/screenshot`), 'base64'));
  tests++;
  await execute("document.getElementById('tab-benchmarks').click();");
  await until('document.querySelectorAll(".operation-row").length === 2');
  await execute("setTimeout(()=>document.querySelector('.operation-row button').click(),0);");
  await delay(200);
  await command(`/session/${session}/alert/dismiss`, {});
  assert.equal(calls.operation.length, 0, 'Declining confirmation must not launch a job.');
  await execute("setTimeout(()=>document.querySelector('.operation-row button').click(),0);");
  await delay(200);
  await command(`/session/${session}/alert/accept`, {});
  await until('document.querySelector(".job .badge")?.textContent === "PASS"');
  assert.deepEqual(calls.operation, [{ action_id: 'api-smoke', confirm: true }]);
  tests++;
  await execute("document.getElementById('tab-workspace').click();");
  await until('!document.getElementById("workspace-create").disabled');
  await execute("document.getElementById('workspace-root').value='/fixture/repo';setTimeout(()=>document.getElementById('workspace-form').requestSubmit(),0);");
  await delay(200); await command(`/session/${session}/alert/accept`, {});
  await until('document.getElementById("workspace-status").textContent.includes("CONNECTED")');
  assert.equal(calls.workspace.length, 1);
  assert.equal(await execute('return document.getElementById("workspace-send").disabled'), true, 'Pi prompt capability false must disable sending.');
  await execute("document.getElementById('workspace-files-form').requestSubmit();");
  await until('document.querySelectorAll(".file-entry").length === 1');
  await execute("document.querySelector('.file-entry').click();");
  await until('document.getElementById("workspace-file-content").textContent.includes("onerror")');
  assert.equal(await execute('return !!document.querySelector("#workspace-file-content img")'), false);
  await execute("setTimeout(()=>document.getElementById('workspace-terminal-form').requestSubmit(),0);");
  await delay(200); await command(`/session/${session}/alert/accept`, {});
  await until('document.getElementById("workspace-terminal-output").textContent.includes("Exit: 0")');
  assert.equal(calls.terminal, 1);
  assert.equal(await execute('return document.getElementById("workspace-shell-run").disabled'), true);
  await execute("setTimeout(()=>document.getElementById('workspace-start').click(),0);");
  await delay(200); await command(`/session/${session}/alert/accept`, {});
  await until('!document.getElementById("workspace-shell-run").disabled');
  await execute("document.getElementById('workspace-shell-command').value='printf shell-test';setTimeout(()=>document.getElementById('workspace-shell-form').requestSubmit(),0);");
  await delay(200); await command(`/session/${session}/alert/dismiss`, {});
  assert.equal(calls.shell.length, 0);
  await execute("setTimeout(()=>document.getElementById('workspace-shell-form').requestSubmit(),0);");
  await delay(200); await command(`/session/${session}/alert/accept`, {});
  await until('document.getElementById("workspace-terminal-output").textContent.includes("custom fixture output")');
  assert.deepEqual(calls.shell, [{ command: 'printf shell-test', confirm: true }]);
  tests++;
  assert.equal(calls.workspace[0].mode, 'protected');
  assert.equal(calls.workspace[0].allow_direct, undefined);
  assert.equal(calls.workspace[0].conversation_id, calls.chat[0].conversation_id);
  assert.equal(await execute(`return document.getElementById("workspace-effective").textContent.includes(${JSON.stringify(`actual reasoning: ${calls.workspace[0].reasoning_effort}`)})`), true);
  await execute("document.getElementById('reasoning-mode').value='max';document.getElementById('reasoning-mode').dispatchEvent(new Event('change'));");
  assert.equal(await execute(`return document.getElementById("workspace-effective").textContent.includes(${JSON.stringify(`actual reasoning: ${calls.workspace[0].reasoning_effort}`)})`), true, 'Changing the chat/new-session selector must not relabel an existing Pi process.');
  assert.equal(await execute('return document.getElementById("workspace-verdict").textContent'), 'UNVERIFIED');
  assert.equal(await execute('return document.getElementById("workspace-apply").disabled'), true);
  await execute("setTimeout(()=>document.getElementById('pi-handoff').click(),0);");
  await delay(200); await command(`/session/${session}/alert/accept`, {});
  for (let wait = 0; handoffs < 1 && wait < 50; wait++) await delay(100);
  assert.equal(handoffs, 1); assert.equal(calls.chat.length, 2); assert.equal(piPrompts, 0, 'Staging history must not execute Pi.');
  tests++;
  await execute("document.getElementById('workspace-prompt').value='Review the parser fixture';setTimeout(()=>document.getElementById('workspace-prompt-form').requestSubmit(),0);");
  await delay(200); await command(`/session/${session}/alert/accept`, {});
  await until('document.getElementById("workspace-messages").textContent.includes("Pi fixture parser review complete.")');
  assert.equal(piPrompts, 1);
  await execute("document.getElementById('pi-to-chat').click();");
  await until('document.getElementById("conversation").textContent.includes("Pi fixture parser review complete.")');
  assert.equal(await execute('return document.getElementById("conversation").textContent.includes("Working code")'), true);
  assert.equal(calls.chat.length, 2, 'Opening Pi history in Chat must not run inference.');
  await execute("setTimeout(()=>document.getElementById('delete-conversation').click(),0);");
  await delay(200); await command(`/session/${session}/alert/accept`, {});
  await until('document.getElementById("global-notice").textContent.includes("Close linked workspace first")');
  assert.equal(conversationDeletes, 0, 'An active linked workspace prevents deletion.');
  tests++;
  await execute("document.getElementById('tab-workspace').click();");
  await writeFile(join(output, 'workspace-desktop.png'), Buffer.from(await command(`/session/${session}/screenshot`), 'base64'));
  tests++;
  await execute("document.getElementById('tab-options').click();");
  await until('!document.getElementById("api-settings-fields").disabled');
  assert.equal(await execute('return document.title'), 'HaloClu');
  assert.equal(await execute('return document.getElementById("ui-language").closest("[role=tabpanel]").id'), 'panel-options');
  assert.equal(await execute('return document.getElementById("settings-listen").textContent'), '127.0.0.1:18093');
  await execute("document.getElementById('ui-language').value='it';document.getElementById('ui-language').dispatchEvent(new Event('change'));");
  assert.equal(await execute('return document.getElementById("workspace-create").textContent'), 'Crea sessione');
  assert.equal(await execute('return JSON.parse(localStorage.getItem("haloclu.preferences")).language'), 'it');
  await execute("document.getElementById('ui-language').value='en';document.getElementById('ui-language').dispatchEvent(new Event('change'));");
  await execute("document.getElementById('ui-text-size').value='18';document.getElementById('ui-text-size').dispatchEvent(new Event('change'));document.getElementById('ui-density').value='compact';document.getElementById('ui-density').dispatchEvent(new Event('change'));document.getElementById('show-thinking').checked=true;document.getElementById('show-thinking').dispatchEvent(new Event('change'));");
  assert.equal(await execute('return getComputedStyle(document.documentElement).getPropertyValue("--conversation-size").trim()'), '18px');
  assert.equal(await execute('return document.body.dataset.density'), 'compact');
  assert.equal(await execute('return JSON.parse(localStorage.getItem("haloclu.preferences")).expand_thinking'), true);
  tests++;
  await execute("document.getElementById('api-operations').checked=false;setTimeout(()=>document.getElementById('api-settings-form').requestSubmit(),0);");
  await delay(200); await command(`/session/${session}/alert/dismiss`, {});
  assert.equal(calls.settings.length, 0);
  await execute("setTimeout(()=>document.getElementById('api-settings-form').requestSubmit(),0);");
  await delay(200); await command(`/session/${session}/alert/accept`, {});
  await until('document.getElementById("settings-result").textContent === "API controls saved."');
  assert.deepEqual(calls.settings, [{ api: { chat: true, workspaces: true, legacy_coding: true, operations: false } }]);
  tests++;
  await execute("setTimeout(()=>document.getElementById('rotate-api-token').click(),0);");
  await delay(200); await command(`/session/${session}/alert/dismiss`, {});
  assert.equal(calls.rotations, 0);
  deferStatusFailure = true;
  await execute("document.getElementById('refresh-health').click();");
  for (let wait = 0; !releaseStatusFailure && wait < 50; wait++) await delay(100);
  assert.equal(typeof releaseStatusFailure, 'function', 'The old-credential health request must be in flight.');
  await execute("setTimeout(()=>document.getElementById('rotate-api-token').click(),0);");
  await delay(200); await command(`/session/${session}/alert/accept`, {});
  await until('document.getElementById("connection-result").textContent.startsWith("Token rotated.")');
  assert.equal(calls.rotations, 1);
  assert.equal(await execute('return document.getElementById("api-token").value'), apiToken);
  releaseStatusFailure(); releaseStatusFailure = null;
  await until('!document.getElementById("refresh-health").disabled');
  assert.equal(await execute('return document.getElementById("connection-label").textContent'), 'Local API connected', 'Old in-flight401 must not log out or overwrite new authenticated state.');
  assert.equal(await execute('return document.getElementById("connection-result").textContent.startsWith("Token rotated.")'), true);
  tests++;
  await execute("document.getElementById('refresh-settings').click();");
  await until('document.getElementById("settings-result").textContent === "Server settings loaded."');
  assert.equal(await execute('return localStorage.getItem("haloclu.preferences").includes("fixture")'), false);
  tests++;
  await execute("document.getElementById('tab-benchmarks').click();");
  await until('document.querySelectorAll(".operation-row").length === 2');
  await execute("setTimeout(()=>document.querySelector('.operation-row button').click(),0);");
  await delay(200); await command(`/session/${session}/alert/accept`, {});
  await until('document.getElementById("global-notice").textContent.includes("paused in Options")');
  assert.equal(calls.operation.length, 1, 'Disabled operation must not be started.');
  assert.equal(await execute('return document.getElementById("panel-benchmarks").hidden'), false, 'A disabled API is not an authentication failure.');
  await execute("document.getElementById('tab-options').click();");
  await until('!document.getElementById("api-settings-fields").disabled');
  assert.notEqual(await execute('return document.getElementById("connection-label").textContent'), 'Token required');
  tests++;
  await writeFile(join(output, 'options-desktop.png'), Buffer.from(await command(`/session/${session}/screenshot`), 'base64'));
  tests++;
  await command(`/session/${session}/window/rect`, { width: 390, height: 844 });
  await execute("document.getElementById('sidebar-toggle').click();");
  for (const tab of ['chat', 'workspace', 'coding', 'models', 'benchmarks', 'cluster', 'options']) {
    await execute(`document.getElementById('tab-${tab}').click();`);
    assert.equal(await execute('return document.documentElement.scrollWidth <= window.innerWidth'), true, `Mobile horizontal overflow: ${tab}`);
  }
  await writeFile(join(output, 'options-mobile.png'), Buffer.from(await command(`/session/${session}/screenshot`), 'base64'));
  tests++;
  await execute("document.getElementById('open-connection').click();document.getElementById('forget-token').click();");
  assert.equal(await execute('return document.getElementById("api-token").value'), '');
  assert.equal(await execute('return localStorage.length + sessionStorage.length'), 1);
  assert.equal(await execute('return localStorage.key(0)'), 'haloclu.preferences');
  assert.deepEqual(await execute('return Object.keys(JSON.parse(localStorage.getItem("haloclu.preferences"))).sort()'), ['density', 'expand_thinking', 'language', 'show_advanced', 'sidebar_collapsed', 'text_size']);
  tests++;
  await command(`/session/${session}/refresh`, {});
  await until('!document.getElementById("panel-options").hidden');
  assert.equal(await execute('return document.getElementById("api-token").value'), '', 'Credentials must not survive reload.');
  assert.equal(await execute('return document.getElementById("api-settings-fields").disabled'), true);
  assert.equal(await execute('return document.getElementById("ui-text-size").value'), '18');
  assert.equal(await execute('return document.body.dataset.density'), 'compact');
  assert.equal(await execute('return document.getElementById("show-thinking").checked'), true);
  assert.equal(await execute('return document.getElementById("ui-show-advanced").checked'), true);
  assert.equal(await execute('return document.getElementById("generation-settings").hidden'), true, 'Options never shows Generation after reload.');
  tests++;
  await execute(`document.getElementById('api-token').value=${JSON.stringify(apiToken)};document.getElementById('connection-form').requestSubmit();`);
  await until('document.getElementById("connection-label").textContent === "Local API connected" && document.querySelectorAll(".conversation-item").length === 1');
  await execute("document.querySelector('.conversation-item').click();");
  await until('document.getElementById("conversation").textContent.includes("Pi fixture parser review complete.")');
  assert.equal(await execute('return document.getElementById("conversation").textContent.includes("Working code")'), true);
  assert.equal(calls.chat.length, 2); assert.equal(piPrompts, 1, 'Reload recovers both histories without model/agent replay.');
  tests++;
  deferConversationRead = true;
  await execute("document.querySelector('.conversation-item').click();");
  for (let wait = 0; !releaseConversationRead && wait < 50; wait++) await delay(100);
  assert.equal(typeof releaseConversationRead, 'function');
  await execute("document.getElementById('clear-chat').click();");
  releaseConversationRead(); releaseConversationRead = null;
  await delay(200);
  assert.equal(await execute('return document.querySelectorAll("#conversation .message").length'), 0, 'A stale history response must not reopen a conversation after New.');
  await execute("document.querySelector('.conversation-item').click();");
  await until('document.getElementById("conversation").textContent.includes("Pi fixture parser review complete.")');
  tests++;
  await execute("document.getElementById('tab-workspace').click();");
  await until('document.querySelector("#workspace-session option[value=ws-fixture]") !== null');
  await execute("document.getElementById('workspace-session').value='ws-fixture';document.getElementById('workspace-session').dispatchEvent(new Event('change'));");
  await until('document.getElementById("workspace-status").textContent.includes("READY")');
  await execute("setTimeout(()=>document.getElementById('workspace-close').click(),0);");
  await delay(200); await command(`/session/${session}/alert/accept`, {});
  await until('document.getElementById("workspace-status").textContent.includes("CLOSED")');
  await execute("setTimeout(()=>document.getElementById('delete-conversation').click(),0);");
  await delay(200); await command(`/session/${session}/alert/accept`, {});
  await until('document.querySelectorAll(".conversation-item").length === 0 && document.getElementById("workspace-messages").textContent === ""');
  assert.equal(conversationDeletes, 1); assert.equal(conversations.size, 0); assert.equal(workspace, null);
  assert.equal(calls.chat.length, 2); assert.equal(piPrompts, 1);
  tests++;
  const summary = { result: 'PASS', browser: 'installed Firefox via WebDriver', checks: tests, backend: 'deterministic fixture, not live GLM', real_model_calls: 0, fixture_calls: { chat: calls.chat.length, coding: calls.coding.length, apply: calls.apply, cancel: calls.cancel, operation: calls.operation.length, settings: calls.settings.length, rotations: calls.rotations }, output };
  await writeFile(join(output, 'summary.json'), JSON.stringify(summary, null, 2) + '\n');
  console.log(JSON.stringify(summary, null, 2));
} finally {
  if (session) await fetch(`${base}/session/${session}`, { method: 'DELETE', signal: AbortSignal.timeout(10000) }).catch(() => {});
  driver.kill('SIGTERM');
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
  await writeFile(join(output, 'geckodriver.log'), driverLog);
}
