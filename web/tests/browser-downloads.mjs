// Standalone Firefox test of the native download UI with metadata/job fixtures.
// No model files, packages, browser downloads or live credentials are involved.
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createServer } from 'node:http';
import { readFile, mkdir, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { resolve, join } from 'node:path';

const output = resolve(process.argv[2] || join(tmpdir(), `haloclu-download-ui-${Date.now()}`));
await mkdir(output, { recursive: true });
const jobID = 'a'.repeat(32), revision = 'b'.repeat(40), sha256 = 'c'.repeat(64);
const counts = { search: 0, files: 0, plan: 0, jobs: 0, start: 0, pause: 0, resume: 0, cancel: 0 };
let job = null;
const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url, 'http://127.0.0.1'); const chunks = []; for await (const chunk of req) chunks.push(chunk);
    const body = chunks.length ? JSON.parse(Buffer.concat(chunks)) : {};
    const json = (value, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(value)); };
    if (url.pathname === '/') { res.writeHead(200, { 'Content-Type': 'text/html' }); return res.end(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Download UI fixture</title><link rel="stylesheet" href="/styles.css"></head><body><main style="max-width:1000px;margin:30px auto"><section id="panel-models"><section id="models-catalog"><h2>Local and tested</h2><div id="models-list">Sign in to inspect the catalog.</div></section></section><p id="notice"></p></main><script type="module">import {initDownloads} from '/downloads.mjs';const request=async(path,options={})=>{const response=await fetch(path,{method:options.method||'GET',headers:{'Content-Type':'application/json','Authorization':'Bearer public-fixture-only'},body:options.body?JSON.stringify(options.body):undefined});const value=await response.json();if(!response.ok)throw new Error(value.error||'fixture error');return value};window.fixtureAuthenticated=false;window.downloadUI=initDownloads({request,notice:message=>document.getElementById('notice').textContent=message,bytes:value=>value+' B',seconds:value=>value.toFixed(1)+' s',isAuthenticated:()=>window.fixtureAuthenticated});await window.downloadUI.refresh();</script></body></html>`); }
    if (url.pathname === '/downloads.mjs' || url.pathname === '/styles.css') { res.writeHead(200, { 'Content-Type': url.pathname.endsWith('.css') ? 'text/css' : 'text/javascript' }); return res.end(await readFile(new URL(`..${url.pathname}`, import.meta.url))); }
    assert.equal(req.headers.authorization, 'Bearer public-fixture-only');
    if (url.pathname === '/v1/downloads/options') return json({ sources: ['huggingface', 'modelscope'], destination_root: '/fixture/downloads', concurrency: 1, disk_reserve_bytes: 17179869184 });
    if (url.pathname === '/v1/downloads/jobs') { counts.jobs++; return json({ jobs: job ? [job] : [] }); }
    if (url.pathname === '/v1/downloads/search') { counts.search++; assert.equal(url.searchParams.get('source'), 'all'); return json({ items: [{ source: 'huggingface', repo: 'fixture/model', tags: ['gguf'], url: 'https://huggingface.co/fixture/model' }], warnings: { modelscope: 'Fixture source outage; Hugging Face results retained.' } }); }
    if (url.pathname === '/v1/downloads/files') { counts.files++; return json({ source: 'huggingface', repo: 'fixture/model', revision, files: [{ path: 'model.gguf', size: 6, sha256, revision, url: `https://huggingface.co/fixture/model/resolve/${revision}/model.gguf` }, { path: 'README.md', size: 12, revision, url: `https://huggingface.co/fixture/model/resolve/${revision}/README.md` }], warning: 'Fixture metadata, not a model qualification.' }); }
    if (url.pathname === '/v1/downloads/plan') { counts.plan++; assert.deepEqual(body, { source: 'huggingface', repo: 'fixture/model', revision, files: ['model.gguf'] }); job = { id: jobID, source: body.source, repo: body.repo, revision, status: 'PLANNED', destination: `/fixture/downloads/${jobID}/data`, files: [{ path: 'model.gguf', size: 6, bytes: 0, sha256, status: 'PLANNED' }], bytes: 0, total_bytes: 6, bytes_per_second: 0, eta_seconds: null, warning: 'Fixture only; no transfer.' }; return json(job, 201); }
    const action = url.pathname.split('/').at(-1);
    if (['start', 'pause', 'resume', 'cancel'].includes(action)) {
      assert.deepEqual(body, { confirm: true }); counts[action]++;
      job.status = { start: 'RUNNING', pause: 'PAUSED', resume: 'RUNNING', cancel: 'CANCELLED' }[action]; job.bytes = 3; job.files[0].bytes = 3; job.files[0].status = job.status;
      job.bytes_per_second = job.status === 'RUNNING' ? 2 : 0; job.eta_seconds = job.status === 'RUNNING' ? 1.5 : null; return json(job, 202);
    }
    json({ error: 'Unknown fixture route' }, 404);
  } catch (error) { res.writeHead(500, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ error: error.message })); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const holder = createServer(); await new Promise(resolve => holder.listen(0, '127.0.0.1', resolve)); const driverPort = holder.address().port; await new Promise(resolve => holder.close(resolve));
const driver = spawn('geckodriver', ['--host', '127.0.0.1', '--port', String(driverPort)], { stdio: ['ignore', 'pipe', 'pipe'] });
let log = '', session = ''; driver.stdout.on('data', chunk => { log += chunk; }); driver.stderr.on('data', chunk => { log += chunk; }); driver.on('error', error => { log += error.stack; });
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
async function command(path, body) { const response = await fetch(`http://127.0.0.1:${driverPort}${path}`, { method: body === undefined ? 'GET' : 'POST', headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(30000) }); const value = await response.json(); if (!response.ok || value.value?.error) throw new Error(JSON.stringify(value)); return value.value; }
const execute = script => command(`/session/${session}/execute/sync`, { script, args: [] });
async function until(expression) { for (let n = 0; n < 100; n++) { if (await execute(`return Boolean(${expression})`)) return; await delay(100); } throw new Error(`Timed out: ${expression}`); }
const result = { status: 'FAIL', checks: [], fixture_only: true, model_requests: 0, model_downloads: 0, counts };
try {
  for (let n = 0; n < 100; n++) { try { await command('/status'); break; } catch { if (n === 99) throw new Error('geckodriver unavailable'); await delay(100); } }
  session = (await command('/session', { capabilities: { alwaysMatch: { browserName: 'firefox', 'moz:firefoxOptions': { args: ['-headless'] } } } })).sessionId;
  await command(`/session/${session}/window/rect`, { width: 1280, height: 1100 }); await command(`/session/${session}/url`, { url: `http://127.0.0.1:${server.address().port}/` });
  await until('document.getElementById("download-query")?.disabled');
  assert.equal(counts.jobs, 0);
  await execute("document.getElementById('download-query').value='fixture';document.getElementById('download-search-form').requestSubmit();");
  assert.equal(counts.search, 0); assert.equal(counts.plan, 0);
  result.checks.push('signed-out controls are disabled and programmatic submit performs no authenticated metadata request');
  await execute("window.fixtureAuthenticated=true;window.downloadUI.syncAuthentication();window.downloadUI.refresh();");
  await until('document.getElementById("download-storage")?.textContent.includes("/fixture/downloads")');
  const before = await execute('return document.getElementById("download-query").getBoundingClientRect().top');
  await execute("document.getElementById('models-list').replaceChildren(...Array.from({length:12},()=>{const p=document.createElement('p');p.style.height='200px';p.textContent='Catalog fixture';return p}));");
  assert.equal(await execute('return document.getElementById("download-query").getBoundingClientRect().top'), before);
  assert.equal(await execute('return document.getElementById("model-downloads").nextElementSibling.id'), 'models-catalog');
  result.checks.push('catalog expansion does not displace the downloader; both keep independent DOM roots');
  assert.equal(counts.start, 0); assert.equal(counts.plan, 0); result.checks.push('initialization reads status only');
  await execute("document.getElementById('download-query').value='fixture';document.getElementById('download-search-form').requestSubmit();");
  await until('document.getElementById("download-feedback").textContent.includes("Fixture source outage")'); assert.equal(counts.search, 1); result.checks.push('combined search preserves per-source warning and successful results');
  await execute("document.querySelector('#download-search-results button').click();"); await until('document.querySelectorAll("#download-file-list input").length===2');
  assert.equal(await execute('return document.querySelectorAll("#download-file-list input:checked").length'), 0);
  await execute("document.getElementById('download-file-0').click();document.getElementById('download-plan-selected').click();"); await until('document.querySelector(".download-job .badge")?.textContent==="PLANNED"');
  assert.equal(counts.start, 0); assert.equal(counts.plan, 1); assert.equal(await execute('return document.getElementById("download-pin").textContent.includes("' + revision + '")'), true); result.checks.push('explicit selected-file pinned plan, no transfer');
  async function clickAction(label, accept = true) {
    await execute(`setTimeout(()=>[...document.querySelectorAll('.download-job button')].find(button=>button.textContent===${JSON.stringify(label)}).click(),0);`); await delay(80);
    const prompt = await command(`/session/${session}/alert/text`); assert.ok(prompt.includes('No model will be loaded or switched.'));
    await command(`/session/${session}/alert/${accept ? 'accept' : 'dismiss'}`, {});
  }
  await clickAction('Start', false); assert.equal(counts.start, 0); result.checks.push('dismissed start confirmation sends no request');
  await clickAction('Start'); await until('document.querySelector(".download-job .badge").textContent==="RUNNING"'); assert.equal(counts.start, 1);
  assert.equal(await execute('return document.querySelector(".download-job progress").value'), .5); assert.ok(await execute('return document.querySelector(".download-job").textContent.includes("2 B/s")')); result.checks.push('confirmed start renders server bytes/rate/ETA');
  await clickAction('Pause'); await until('document.querySelector(".download-job .badge").textContent==="PAUSED"'); await clickAction('Resume'); await until('document.querySelector(".download-job .badge").textContent==="RUNNING"');
  assert.equal(counts.pause, 1); assert.equal(counts.resume, 1); result.checks.push('explicit pause and resume preserve job identity');
  await execute("document.getElementById('panel-models').hidden=true;"); await delay(100); const hiddenCount = counts.jobs; await delay(2300); assert.equal(counts.jobs, hiddenCount); result.checks.push('hidden Models stops polling');
  await execute("document.getElementById('panel-models').hidden=false;window.downloadUI.refresh();"); await until('document.querySelector(".download-job .badge").textContent==="RUNNING"'); await clickAction('Cancel'); await until('document.querySelector(".download-job .badge").textContent==="CANCELLED"'); assert.equal(counts.cancel, 1); assert.equal(job.files[0].bytes, 3); result.checks.push('cancel preserves partial byte receipt and manual resume');
  await execute('window.fixtureAuthenticated=false;window.downloadUI.disconnect();'); assert.equal(await execute('return document.getElementById("download-jobs").textContent'), 'Sign in to inspect preserved downloads.'); assert.equal(await execute('return document.getElementById("download-query").disabled'), true); assert.equal(await execute('return document.getElementById("download-file-list").children.length'), 0); const disconnectedCount = counts.jobs; await delay(2300); assert.equal(counts.jobs, disconnectedCount); result.checks.push('disconnect clears private job view, selection and polling and disables acquisition');
  result.status = 'PASS';
} catch (error) { result.error = error.stack; throw error; }
finally {
  if (session) await fetch(`http://127.0.0.1:${driverPort}/session/${session}`, { method: 'DELETE', signal: AbortSignal.timeout(5000) }).catch(() => {});
  driver.kill('SIGTERM'); await new Promise(resolve => server.close(resolve));
  await writeFile(join(output, 'result.json'), JSON.stringify(result, null, 2)); await writeFile(join(output, 'geckodriver.log'), log); console.log(JSON.stringify({ ...result, raw: output }, null, 2));
}
