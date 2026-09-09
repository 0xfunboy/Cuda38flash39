// Explicit live workspace UI receipt: GETs plus exactly one confirmed shell
// command in an existing disposable session. No model prompts or lifecycle.
// Usage: node web/tests/browser-workspace-live.mjs URL TOKEN_FILE SESSION_ID OUTPUT_DIR --run-live
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createServer } from 'node:http';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const [baseArg, tokenFile, workspaceID, outputArg, consent] = process.argv.slice(2);
assert.equal(consent, '--run-live', 'Explicit live execution opt-in required.');
assert.match(workspaceID || '', /^[a-f0-9]{32}$/);
const apiURL = new URL(baseArg);
assert.equal(apiURL.protocol, 'http:');
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(apiURL.hostname));
assert.equal(apiURL.username + apiURL.password + apiURL.search + apiURL.hash, '');
const token = (await readFile(resolve(tokenFile), 'utf8')).trim();
assert.ok(token.length > 0);
const output = resolve(outputArg);
await mkdir(output, { recursive: false, mode: 0o700 });
const redact = value => String(value).split(token).join('[REDACTED]');
const report = { started_utc: new Date().toISOString(), endpoint: apiURL.origin, workspace_id: workspaceID, model_calls: 0, lifecycle_calls: 0, shell_command: 'printf workspace-shell-ok', checks: [] };
const sessionPath = `/v1/workspaces/sessions/${workspaceID}`;
async function api(path) {
  const response = await fetch(new URL(path, apiURL), { headers: { Authorization: `Bearer ${token}` }, redirect: 'error', signal: AbortSignal.timeout(15000) });
  assert.equal(response.status, 200, `GET ${path} failed`);
  return JSON.parse(redact(await response.text()));
}
report.preflight = await api(sessionPath);
assert.equal(report.preflight.state, 'READY', 'The authorized session must already be idle and ready.');
assert.equal(report.preflight.capabilities.shell, true);
await writeFile(resolve(output, 'intent.json'), JSON.stringify({ ...report, allowed_writes: [{ method: 'POST', path: `${sessionPath}/terminal`, body: { command: report.shell_command, confirm: true } }] }, null, 2));

const holder = createServer();
await new Promise(done => holder.listen(0, '127.0.0.1', done));
const driverPort = holder.address().port;
await new Promise(done => holder.close(done));
const driver = spawn('geckodriver', ['--host', '127.0.0.1', '--port', String(driverPort)], { stdio: ['ignore', 'pipe', 'pipe'] });
let driverLog = '', browser = '';
driver.stdout.on('data', chunk => { driverLog += chunk; });
driver.stderr.on('data', chunk => { driverLog += chunk; });
driver.on('error', error => { driverLog += error.stack; });
const driverURL = `http://127.0.0.1:${driverPort}`;
const delay = ms => new Promise(done => setTimeout(done, ms));
async function command(path, body) {
  const response = await fetch(driverURL + path, { method: body === undefined ? 'GET' : 'POST', headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(45000) });
  const result = await response.json();
  if (!response.ok || result.value?.error) throw new Error(redact(JSON.stringify(result)));
  return result.value;
}
async function execute(script) { return command(`/session/${browser}/execute/sync`, { script, args: [] }); }
async function until(expression, timeout = 15000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) { if (await execute(`return Boolean(${expression})`)) return; await delay(150); }
  throw new Error(`Browser condition timed out: ${expression}`);
}
async function screenshot(name) {
  assert.equal(await execute('return document.getElementById("connection-panel").hidden && !document.getElementById("workspace-password").value'), true, 'Credential fields must not be captured.');
  await writeFile(resolve(output, name), Buffer.from(await command(`/session/${browser}/screenshot`), 'base64'));
}
try {
  for (let tries = 0; tries < 100; tries++) { try { await command('/status'); break; } catch (error) { if (tries === 99) throw error; await delay(100); } }
  browser = (await command('/session', { capabilities: { alwaysMatch: { browserName: 'firefox', 'moz:firefoxOptions': { args: ['-headless'] } } } })).sessionId;
  await command(`/session/${browser}/window/rect`, { width: 1500, height: 1100 });
  await command(`/session/${browser}/url`, { url: apiURL.href });
  await until('!document.getElementById("connection-panel").hidden');
  await execute(`window.__workspaceAudit={requests:[],responses:[]};const nativeFetch=fetch.bind(window);window.fetch=async(input,init={})=>{const method=(init.method||'GET').toUpperCase();const path=new URL(typeof input==='string'?input:input.url,location.href).pathname;const body=init.body?JSON.parse(init.body):null;if(method!=='GET'&&method!=='HEAD'){if(method!=='POST'||path!==${JSON.stringify(sessionPath + '/terminal')}||JSON.stringify(body)!==JSON.stringify({command:'printf workspace-shell-ok',confirm:true})||window.__workspaceAudit.requests.some(row=>row.method==='POST'))throw new Error('Live audit forbids this write or retry');}window.__workspaceAudit.requests.push({method,path,body});const response=await nativeFetch(input,init);if(path.startsWith('/v1/workspaces/'))response.clone().json().then(value=>window.__workspaceAudit.responses.push({path,status:response.status,value}));return response;};document.getElementById('api-token').value=${JSON.stringify(token)};document.getElementById('connection-form').requestSubmit();`);
  await until('document.getElementById("connection-label").textContent === "Local API connected"');
  await execute("document.getElementById('tab-workspace').click();");
  await until(`Array.from(document.getElementById('workspace-session').options).some(option=>option.value===${JSON.stringify(workspaceID)})`);
  await execute(`document.getElementById('workspace-session').value=${JSON.stringify(workspaceID)};document.getElementById('workspace-session').dispatchEvent(new Event('change'));`);
  await until('document.getElementById("workspace-status").textContent.includes("READY") && !document.getElementById("workspace-shell-run").disabled');
  await until('document.querySelectorAll(".pi-assistant-message").length > 0');
  report.visible_message = await execute('return document.getElementById("workspace-messages").textContent');
  assert.ok(report.visible_message.trim().length > 20);
  report.checks.push('existing real READY Pi session selected; recorded assistant output rendered');
  await screenshot('workspace-existing-message.png');
  await execute("document.getElementById('workspace-files-form').requestSubmit();");
  await until('Array.from(document.querySelectorAll(".file-entry")).some(node=>node.textContent.includes("nth_prime.c"))');
  await execute("Array.from(document.querySelectorAll('.file-entry')).find(node=>node.textContent.includes('nth_prime.c')).click();");
  await until('document.getElementById("workspace-file-content").textContent.includes("nth_prime.c") && document.getElementById("workspace-file-content").textContent.includes("#include")');
  report.file_preview = await execute('return document.getElementById("workspace-file-content").textContent');
  assert.equal(await execute('return document.querySelectorAll("#workspace-file-content script,#workspace-file-content img").length'), 0);
  report.checks.push('real nth_prime.c file browser and safe source preview');
  await execute("document.getElementById('workspace-shell-command').value='printf workspace-shell-ok';setTimeout(()=>document.getElementById('workspace-shell-form').requestSubmit(),0);");
  await delay(250);
  const confirmation = await command(`/session/${browser}/alert/text`);
  assert.match(confirmation, /printf workspace-shell-ok/);
  await command(`/session/${browser}/alert/accept`, {});
  await until('document.getElementById("workspace-terminal-output").textContent.includes("workspace-shell-ok") && document.getElementById("workspace-terminal-output").textContent.includes("Exit: 0")', 70000);
  report.shell_output = await execute('return document.getElementById("workspace-terminal-output").textContent');
  report.checks.push('exact authorized shell command confirmed in browser and completed with exit 0');
  await screenshot('workspace-file-and-shell.png');
  const audit = await execute('return window.__workspaceAudit');
  assert.equal(audit.requests.filter(item => item.method === 'POST').length, 1);
  report.audit = audit;
  report.after = await api(sessionPath);
  assert.equal(report.after.state, 'READY');
  report.result = 'PASS';
  console.log(JSON.stringify({ result: 'PASS', checks: report.checks.length, model_calls: 0, shell_calls: 1, lifecycle_calls: 0, output }, null, 2));
} catch (error) { report.result = 'FAIL'; report.error = redact(error.stack); throw error; }
finally {
  report.finished_utc = new Date().toISOString();
  if (browser) await fetch(`${driverURL}/session/${browser}`, { method: 'DELETE', signal: AbortSignal.timeout(10000) }).catch(() => {});
  driver.kill('SIGTERM');
  await writeFile(resolve(output, 'workspace-live.json'), redact(JSON.stringify(report, null, 2)));
  await writeFile(resolve(output, 'geckodriver.log'), redact(driverLog));
}
