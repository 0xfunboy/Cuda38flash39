import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { apiSettings, displayPreferences, generationPanelState } from '../ui-core.mjs';

test('display preferences are a bounded whitelist without credentials', () => {
  assert.deepEqual(displayPreferences({ language: 'it', text_size: '18', density: 'compact', expand_thinking: true, sidebar_collapsed: true, show_advanced: true, token: 'never-store-this', password: 'private', api: { chat: false } }), { language: 'it', text_size: 18, density: 'compact', expand_thinking: true, sidebar_collapsed: true, show_advanced: true });
  for (const source of [null, {}, 'bad', { language: 'xx', text_size: '100; color:red', density: 'arbitrary', expand_thinking: 'true', show_advanced: 'true' }]) assert.deepEqual(displayPreferences(source), { language: 'en', text_size: 14, density: 'comfortable', expand_thinking: false, sidebar_collapsed: false, show_advanced: false });
});

test('API settings require four explicit booleans and discard unrelated fields', () => {
  const api = { chat: true, workspaces: false, legacy_coding: true, operations: false };
  assert.deepEqual(apiSettings({ ...api, listen: '0.0.0.0', token: 'never' }), api);
  for (const source of [null, {}, { ...api, operations: undefined }, { ...api, chat: 'false' }]) assert.throws(() => apiSettings(source));
});

test('HaloClu options contain global preferences and authentication, not generation controls', async () => {
  const html = await readFile(new URL('../index.html', import.meta.url), 'utf8');
  assert.match(html, /<title>HaloClu<\/title>/);
  assert.match(html, /class="brand" aria-label="HaloClu home"/);
  const options = html.slice(html.indexOf('<section id="panel-options"'), html.indexOf('<section id="panel-chat"'));
  for (const id of ['ui-language', 'show-thinking', 'ui-text-size', 'ui-density', 'ui-sidebar-collapsed', 'ui-show-advanced', 'connection-form', 'api-token', 'rotate-api-token', 'api-settings-form']) assert.ok(options.includes(`id="${id}"`), id);
  for (const id of ['reasoning-mode', 'thinking-budget', 'context-select', 'chat-cap']) assert.equal(options.includes(`id="${id}"`), false);
});

test('Generation is scoped to chat, new Pi sessions and opt-in legacy coding', () => {
  assert.deepEqual(generationPanelState('chat'), { visible: true, workspace: false, fullControls: true });
  assert.deepEqual(generationPanelState('workspace'), { visible: true, workspace: true, fullControls: false });
  assert.deepEqual(generationPanelState('coding', true), { visible: true, workspace: false, fullControls: true });
  for (const tab of ['models', 'benchmarks', 'cluster', 'options', 'unknown', 'coding']) assert.deepEqual(generationPanelState(tab), { visible: false, workspace: false, fullControls: false });
});

test('Advanced is initially hidden and keyboard navigation uses only visible sections', async () => {
  const html = await readFile(new URL('../index.html', import.meta.url), 'utf8');
  const script = await readFile(new URL('../app.js', import.meta.url), 'utf8');
  assert.match(html, /id="tab-coding"[^>]* hidden>/);
  assert.match(script, /if \(state\.activeTab === 'coding' && !state\.preferences\.show_advanced\) selectTab\('chat'\)/);
  assert.match(script, /querySelectorAll\('\[data-tab\]'\)\]\.filter\(node => !node\.hidden\)/);
  assert.match(html, /Existing sessions keep their captured reasoning/);
  const rendering = script.slice(script.indexOf('function renderGenerationVisibility'), script.indexOf('function selectTab'));
  assert.doesNotMatch(rendering, /\.value\s*=|\.disabled\s*=/);
});

test('Supplied application marks have local URLs, dimensions and accessible branding', async () => {
  const html = await readFile(new URL('../index.html', import.meta.url), 'utf8');
  assert.match(html, /rel="icon"[^>]*href="\.\/assets\/haloclu-icon\.png"/);
  assert.match(html, /class="brand-wordmark"[^>]*alt="HaloClu"[^>]*width="2508" height="627"/);
  assert.match(html, /class="brand-icon"[^>]*alt=""[^>]*width="800" height="800"/);
});
