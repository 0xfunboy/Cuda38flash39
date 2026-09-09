import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { downloadJobActions, downloadProgress } from '../downloads.mjs';

test('Download actions preserve explicit start/resume and never imply deletion', () => {
  assert.deepEqual(downloadJobActions('PLANNED'), ['start', 'cancel']);
  for (const status of ['PAUSED', 'FAILED', 'CANCELLED']) assert.deepEqual(downloadJobActions(status), ['resume']);
  for (const status of ['RUNNING', 'VERIFYING']) assert.deepEqual(downloadJobActions(status), ['pause', 'cancel']);
  for (const status of ['COMPLETE', 'PAUSING', 'CANCELLING', undefined]) assert.deepEqual(downloadJobActions(status), []);
});

test('Download progress uses server byte counters, not fabricated transfer speed', () => {
  assert.deepEqual(downloadProgress({ bytes: 30, total_bytes: 100 }), { bytes: 30, total: 100, fraction: .3 });
  assert.equal(downloadProgress({ bytes: 150, total_bytes: 100 }).fraction, 1);
  assert.equal(downloadProgress({ bytes: 0, total_bytes: 0, status: 'PLANNED' }).fraction, 0);
  assert.equal(downloadProgress({ bytes: 0, total_bytes: 0, status: 'COMPLETE' }).fraction, 1);
  assert.equal(downloadProgress({}).total, null);
});

test('Native downloader UI has no execution, unsafe HTML, secret persistence or automatic transfer', async () => {
  const source = await readFile(new URL('../downloads.mjs', import.meta.url), 'utf8');
  assert.doesNotMatch(source, /innerHTML|outerHTML|insertAdjacentHTML|localStorage|sessionStorage|\beval\(/);
  assert.match(source, /window\.confirm/);
  assert.match(source, /body: \{ confirm: true \}/);
  assert.match(source, /!panel\.hidden && !document\.hidden/);
  assert.match(source, /No model will be loaded or switched/);
  assert.match(source, /No automatic retry/);
});

test('Models keeps acquisition outside the catalog renderer and gates both sections on authentication', async () => {
  const source = await readFile(new URL('../downloads.mjs', import.meta.url), 'utf8');
  const app = await readFile(new URL('../app.js', import.meta.url), 'utf8');
  assert.match(source, /panel\.insertBefore\(section, document\.getElementById\('models-catalog'\)\)/);
  assert.match(source, /n\.disabled = !isAuthenticated\(\) \|\| state\.busy/);
  assert.match(source, /if \(!isAuthenticated\(\)\) \{ disconnect\(\); return; \}/);
  assert.match(app, /isAuthenticated: \(\) => state\.authenticated/);
  assert.match(app, /Promise\.all\(\[refreshCatalog\(\), downloader\.refresh\(\)\]\)/);
  assert.match(app, /\$\('models-list'\)\.replaceChildren/);
  assert.doesNotMatch(app, /\$\('panel-models'\)\.(replaceChildren|innerHTML|textContent)/);
});
