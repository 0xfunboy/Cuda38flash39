// Explicit opt-in browser session check. Only login/logout and read-only APIs.
// No token rotation, inference, downloads, workspace actions or host lifecycle.
// node web/tests/browser-auth-live.mjs URL TOKEN_FILE ARTIFACT_DIR --run-live
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createServer } from 'node:http';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const [baseArg, tokenArg, outputArg, optIn] = process.argv.slice(2);
if (!baseArg || !tokenArg || !outputArg || optIn !== '--run-live') throw new Error('Usage: browser-auth-live.mjs URL TOKEN_FILE ARTIFACT_DIR --run-live');
const origin = new URL(baseArg);
assert.ok(['http:', 'https:'].includes(origin.protocol));
assert.ok(['127.0.0.1', '[::1]', 'localhost'].includes(origin.hostname), 'Loopback gateway only.');
assert.equal(origin.username + origin.password + origin.search + origin.hash, '');
const token = (await readFile(resolve(tokenArg), 'utf8')).trim();
assert.ok(token.length > 0, 'Empty local token.');
const secrets = [token];
const redact = input => secrets.reduce((text, secret) => text.split(secret).join('[REDACTED]'), String(input));
const output = resolve(outputArg); await mkdir(output, { recursive: true });
const holder = createServer(); await new Promise(resolve => holder.listen(0, '127.0.0.1', resolve));
const port = holder.address().port; await new Promise(resolve => holder.close(resolve));
const driver = spawn('geckodriver', ['--host', '127.0.0.1', '--port', String(port)], { stdio: ['ignore', 'pipe', 'pipe'] });
let session = '', log = '';
driver.stdout.on('data', data => { log += data; }); driver.stderr.on('data', data => { log += data; }); driver.on('error', error => { log += error.message; });
const driverURL = `http://127.0.0.1:${port}`;
const delay = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
async function command(path, data, method) {
  const response = await fetch(driverURL + path, { method: method || (data === undefined ? 'GET' : 'POST'), headers: { 'Content-Type': 'application/json' }, body: data === undefined ? undefined : JSON.stringify(data), signal: AbortSignal.timeout(45000) });
  const payload = await response.json();
  if (!response.ok || payload.value?.error) throw new Error(redact(JSON.stringify(payload)));
  return payload.value;
}
async function execute(script) { return command(`/session/${session}/execute/sync`, { script, args: [] }); }
async function until(expression) {
  for (let n = 0; n < 100; n++) { if (await execute(`return Boolean(${expression})`)) return; await delay(100); }
  throw new Error(`Browser condition timed out: ${expression}`);
}
async function guard() {
  await execute(`if(!window.__haloAuthGuard){window.__haloAuthGuard=true;window.__authRequests=[];const real=window.fetch.bind(window);window.fetch=(input,init={})=>{const method=(init.method||input.method||'GET').toUpperCase();const path=new URL(typeof input==='string'?input:input.url,location.href).pathname;window.__authRequests.push({method,path});if(!['GET','HEAD'].includes(method)&&!(path==='/v1/auth/session'&&['POST','DELETE'].includes(method)))return Promise.reject(new Error('Auth audit refuses unrelated mutation'));return real(input,init);};}`);
}
const result = { result: 'RUNNING', api: origin.origin, browser: 'installed Firefox via WebDriver', real_model_calls: 0, token_rotations: 0, lifecycle_changes: 0, checks: [] };
try {
  for (let n = 0; n < 100; n++) { try { await command('/status'); break; } catch (error) { if (n === 99) throw error; await delay(100); } }
  session = (await command('/session', { capabilities: { alwaysMatch: { browserName: 'firefox', 'moz:firefoxOptions': { args: ['-headless'] } } } })).sessionId;
  await command(`/session/${session}/window/rect`, { width: 1440, height: 1050 });
  await command(`/session/${session}/url`, { url: origin.href });
  await until('!document.getElementById("panel-options").hidden'); await guard();
  await until('!document.getElementById("refresh-health").disabled');
  await execute("document.getElementById('tab-models').click();");
  assert.equal(await execute('return document.getElementById("model-downloads").getClientRects().length > 0'), true);
  assert.equal(await execute('return document.getElementById("download-query").disabled'), true);
  assert.equal(await execute('return document.querySelectorAll(".catalog-card").length'), 0);
  assert.equal(await execute('return /connect|sign in/i.test(document.getElementById("download-storage").textContent)'), true);
  assert.equal(await execute('return window.__authRequests.some(r=>r.path==="/v1/catalog"||r.path.startsWith("/v1/downloads/"))'), false);
  result.checks.push('Signed-out Models shows both functions without fetching private catalog, destination or job metadata.');
  await execute("document.getElementById('open-connection').click();");
  await execute(`document.getElementById('api-token').value=${JSON.stringify(token)};document.getElementById('connection-form').requestSubmit();`);
  await until('document.getElementById("connection-label").textContent === "Local API connected"');
  assert.equal(await execute('return document.getElementById("api-token").value === ""'), true, 'API credential must be cleared after login.');
  assert.equal(await execute('return document.cookie.includes("haloclu_session")'), false);
  assert.equal(await execute(`return [...Object.values(localStorage),...Object.values(sessionStorage)].some(value=>value.includes(${JSON.stringify(token)}))`), false);
  result.checks.push('Bearer exchanged for browser session; no API token left in input or Web Storage.');

  await command(`/session/${session}/refresh`, {});
  await until('document.getElementById("connection-label").textContent === "Local API connected"'); await guard();
  assert.equal(await execute('return document.getElementById("api-token").value === ""'), true);
  await execute("document.getElementById('tab-chat').click();");
  assert.equal(await execute('return document.getElementById("reasoning-mode").disabled'), false);
  assert.equal(await execute('return document.getElementById("context-select").disabled'), false);
  await execute("document.getElementById('reasoning-mode').value='max';document.getElementById('reasoning-mode').dispatchEvent(new Event('change'));const c=document.getElementById('context-select');c.value=c.options[0].value;c.dispatchEvent(new Event('change'));");
  assert.equal(await execute('return document.getElementById("reasoning-mode").value'), 'max');
  result.checks.push('F5 restores authenticated API access; reasoning/context controls remain editable without inference.');
  await execute("document.getElementById('tab-models').click();");
  await until('document.querySelectorAll(".catalog-card").length > 0 && !document.getElementById("download-query").disabled');
  assert.equal(await execute('return document.getElementById("models-list").getClientRects().length > 0 && document.getElementById("model-downloads").getClientRects().length > 0'), true);
  assert.equal(await execute('const box=document.getElementById("download-query").getBoundingClientRect();return box.top >= 0 && box.bottom <= innerHeight'), true);
  assert.equal(await execute('return window.__authRequests.some(r=>r.method!=="GET"&&r.method!=="HEAD"&&r.path!=="/v1/auth/session")'), false);
  result.checks.push('After remembered login/F5, local catalog and downloader coexist; acquisition search is above fold. No model/download action is submitted.');

  const originalWindow = await command(`/session/${session}/window`);
  const tab = await command(`/session/${session}/window/new`, { type: 'tab' });
  await command(`/session/${session}/window`, { handle: tab.handle });
  // WebDriver cookies are path-scoped. Inspect on /v1 without exposing values
  // to page JavaScript or including them in the result report.
  await command(`/session/${session}/url`, { url: new URL('/v1/auth/session', origin).href });
  const cookies = await command(`/session/${session}/cookie`);
  const cookie = cookies.find(item => item.name === 'haloclu_session');
  assert.ok(cookie, 'Persistent session cookie exists.'); secrets.push(cookie.value);
  assert.equal(cookie.httpOnly, true); assert.equal(cookie.path, '/v1'); assert.equal(cookie.sameSite, 'Strict');
  assert.ok(cookie.expiry > Date.now()/1000 + 86400, 'Cookie is persistent, not tab/session-only.');
  if (origin.protocol === 'https:') assert.equal(cookie.secure, true);
  await command(`/session/${session}/url`, { url: origin.href });
  await until('document.getElementById("connection-label").textContent === "Local API connected"'); await guard();
  assert.equal(await execute('return document.cookie.includes("haloclu_session")'), false);
  assert.equal(await execute('return document.getElementById("api-token").value === ""'), true);
  result.checks.push('New browser tab restores session; cookie is persistent, HttpOnly, SameSite Strict and /v1 scoped.');

  await execute("document.getElementById('open-connection').click();document.getElementById('forget-token').click();");
  await until('document.getElementById("connection-label").textContent !== "Local API connected"');
  const auth = await command(`/session/${session}/execute/async`, { script: `const done=arguments[arguments.length-1];fetch('/v1/auth/session',{headers:{'X-HaloClu-Session':'1'},credentials:'same-origin'}).then(r=>done(r.status)).catch(()=>done(0));`, args: [] });
  assert.equal(auth, 401, 'Logout revokes the browser credential.');
  await command(`/session/${session}/refresh`, {});
  await until('!document.getElementById("panel-options").hidden && document.getElementById("connection-label").textContent !== "Local API connected"');
  assert.equal(await execute('return document.getElementById("api-token").value === ""'), true);
  await command(`/session/${session}/window`, { handle: originalWindow });
  await command(`/session/${session}/refresh`, {});
  await until('!document.getElementById("panel-options").hidden && document.getElementById("connection-label").textContent !== "Local API connected"');
  result.checks.push('Forget logs out; F5 and the original tab cannot recover the revoked session.');
  result.result = 'PASS';
} catch (error) {
  result.result = 'FAIL'; result.error = redact(error.stack || error.message); throw new Error(result.error);
} finally {
  // Also revoke the test profile's cookie if a preceding assertion failed.
  // This is the same allowed browser logout, never global token rotation.
  if (session) await command(`/session/${session}/execute/async`, { script: `const done=arguments[arguments.length-1];fetch('/v1/auth/session',{method:'DELETE',headers:{'X-HaloClu-Session':'1','Content-Type':'application/json'},credentials:'same-origin',referrerPolicy:'same-origin',body:'{}'}).then(r=>done(r.status)).catch(()=>done(0));`, args: [] }).catch(() => {});
  if (session) await fetch(`${driverURL}/session/${session}`, { method: 'DELETE', signal: AbortSignal.timeout(10000) }).catch(() => {});
  driver.kill('SIGTERM');
  await writeFile(resolve(output, 'summary.json'), JSON.stringify(result, null, 2)+'\n');
  await writeFile(resolve(output, 'geckodriver.log'), redact(log));
  console.log(JSON.stringify(result, null, 2));
}
