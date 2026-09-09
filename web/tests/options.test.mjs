import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { apiSettings, displayPreferences } from '../ui-core.mjs';

test('display preferences are a bounded whitelist without credentials', () => {
  assert.deepEqual(displayPreferences({ language: 'it', text_size: '18', density: 'compact', expand_thinking: true, sidebar_collapsed: true, token: 'never-store-this', password: 'private', api: { chat: false } }), { language: 'it', text_size: 18, density: 'compact', expand_thinking: true, sidebar_collapsed: true });
  for (const source of [null, {}, 'bad', { language: 'xx', text_size: '100; color:red', density: 'arbitrary', expand_thinking: 'true' }]) assert.deepEqual(displayPreferences(source), { language: 'en', text_size: 14, density: 'comfortable', expand_thinking: false, sidebar_collapsed: false });
});

test('API settings require four explicit booleans and discard unrelated fields', () => {
  const api = { chat: true, workspaces: false, legacy_coding: true, operations: false };
  assert.deepEqual(apiSettings({ ...api, listen: '0.0.0.0', token: 'never' }), api);
  for (const source of [null, {}, { ...api, operations: undefined }, { ...api, chat: 'false' }]) assert.throws(() => apiSettings(source));
});

test('HaloClu options contain global preferences and authentication, not generation controls', async () => {
  const html = await readFile(new URL('../index.html', import.meta.url), 'utf8');
  assert.match(html, /<title>HaloClu<\/title>/);
  assert.match(html, /class="brand">HaloClu<\/a>/);
  const options = html.slice(html.indexOf('<section id="panel-options"'), html.indexOf('<section id="panel-chat"'));
  for (const id of ['ui-language', 'show-thinking', 'ui-text-size', 'ui-density', 'ui-sidebar-collapsed', 'connection-form', 'api-token', 'rotate-api-token', 'api-settings-form']) assert.ok(options.includes(`id="${id}"`), id);
  for (const id of ['reasoning-mode', 'thinking-budget', 'context-select', 'chat-cap']) assert.equal(options.includes(`id="${id}"`), false);
});
