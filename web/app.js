import {
  SSEParser, activeRequestLabel, bytes, canCancelTask, canRunWorkspaceShell, classifyStatus, completionDelta, completionState, decodeRate,
  apiSettings, displayPreferences, errorMessage, finite, generationPanelState, generationSettings, healthStatus, importedTaskSpec, isSuccess, isTerminal, normalizeTask, number,
  IT_LABELS, markdownBlocks, markdownInline, observedRate, pathList, percent, safeSourceURL, seconds,
} from './ui-core.mjs';

const $ = id => document.getElementById(id);
const state = {
  token: '', model: '', reasoning: 'low', options: null, contextLimit: null,
  chat: [], chatController: null, task: null, taskID: '', taskTimer: null,
  taskGeneration: 0, healthBusy: false, authenticated: false, healthTimer: null,
  importedOptions: {}, conversations: [], conversationID: 0, catalog: null, actions: [], jobs: [], jobTimer: null, operationSubmitting: false,
  language: 'en', records: [], attachments: [], uploading: 0, workspaceOptions: null, workspaceSessions: [], workspace: null, workspaceTimer: null, workspaceSequence: 0, workspaceEvents: [], workspaceBusy: false,
  preferences: displayPreferences(), activeTab: 'chat', settings: null, settingsBusy: false,
};

const staticLabels = [];
function applyLanguage(language) {
  state.language = language === 'it' ? 'it' : 'en';
  document.documentElement.lang = state.language;
  $('ui-language').value = state.language;
  for (const entry of staticLabels) {
    const value = state.language === 'it' ? IT_LABELS[entry.english.trim()] || entry.english.trim() : entry.english.trim();
    const replacement = entry.english.replace(entry.english.trim(), value);
    if (entry.attribute) entry.node.setAttribute(entry.attribute, replacement); else entry.node.data = replacement;
  }
  $('view-label').textContent = $(`tab-${state.activeTab}`)?.querySelector('.nav-label')?.textContent || state.activeTab;
}

function initializeLanguage() {
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) if (IT_LABELS[node.data.trim()]) staticLabels.push({ node, english: node.data });
  for (const node of document.querySelectorAll('[title],[aria-label],[placeholder]')) for (const attribute of ['title', 'aria-label', 'placeholder']) { const english = node.getAttribute(attribute); if (english && IT_LABELS[english.trim()]) staticLabels.push({ node, attribute, english }); }
  let preferences = { sidebar_collapsed: window.matchMedia('(max-width: 760px)').matches };
  try {
    const saved = localStorage.getItem('haloclu.preferences');
    if (saved) preferences = JSON.parse(saved);
    else preferences.language = localStorage.getItem('strixglm.language') || 'en';
  } catch { /* Storage can be disabled or malformed. */ }
  applyPreferences(preferences);
}

function applyPreferences(value) {
  state.preferences = displayPreferences(value);
  applyLanguage(state.preferences.language);
  document.documentElement.style.setProperty('--conversation-size', `${state.preferences.text_size}px`);
  document.body.dataset.density = state.preferences.density;
  $('ui-text-size').value = String(state.preferences.text_size);
  $('ui-density').value = state.preferences.density;
  $('show-thinking').checked = state.preferences.expand_thinking;
  $('ui-show-advanced').checked = state.preferences.show_advanced;
  $('tab-coding').hidden = !state.preferences.show_advanced;
  if (state.activeTab === 'coding' && !state.preferences.show_advanced) selectTab('chat');
  renderGenerationVisibility();
  document.querySelectorAll('.reasoning-details').forEach(node => { node.open = state.preferences.expand_thinking; });
  setSidebar(state.preferences.sidebar_collapsed);
}

function savePreferences() {
  applyPreferences({ language: $('ui-language').value, text_size: $('ui-text-size').value, density: $('ui-density').value, expand_thinking: $('show-thinking').checked, sidebar_collapsed: $('ui-sidebar-collapsed').checked, show_advanced: $('ui-show-advanced').checked });
  try { localStorage.setItem('haloclu.preferences', JSON.stringify(state.preferences)); localStorage.removeItem('strixglm.language'); } catch { /* Preference persistence is optional; secrets are never included. */ }
}

function inlineMarkdown(container, text) {
  for (const token of markdownInline(text)) {
    const node = token.type === 'text' ? document.createTextNode(token.text) : element(token.type === 'link' ? 'a' : token.type, '', token.text);
    if (token.type === 'link') { node.href = token.href; node.target = '_blank'; node.rel = 'noopener noreferrer'; }
    container.append(node);
  }
}

function renderMarkdown(container, source) {
  const fragment = document.createDocumentFragment();
  for (const block of markdownBlocks(source)) {
    if (block.type === 'code') {
      const wrapper = element('div', 'markdown-code');
      const header = element('div', 'code-label'); header.append(element('span', '', block.language || 'text'));
      const copy = element('button', 'button quiet small-button', state.language === 'it' ? 'Copia' : 'Copy'); copy.type = 'button';
      copy.addEventListener('click', async () => { try { await navigator.clipboard.writeText(block.text); copy.textContent = state.language === 'it' ? 'Copiato' : 'Copied'; } catch { notice('Clipboard unavailable. Select the code manually.'); } });
      header.append(copy); const pre = element('pre', 'code-block'); pre.append(element('code', '', block.text)); wrapper.append(header, pre); fragment.append(wrapper); continue;
    }
    if (block.type === 'rule') { fragment.append(element('hr')); continue; }
    if (block.type === 'list') { const list = element(block.ordered ? 'ol' : 'ul'); if (block.ordered) list.start = block.start; for (const item of block.items) { const li = element('li'); inlineMarkdown(li, item); list.append(li); } fragment.append(list); continue; }
    if (block.type === 'table') {
      const wrap = element('div', 'markdown-table'); const table = element('table');
      const head = element('thead'), headRow = element('tr');
      for (const cell of block.header) { const th = element('th'); inlineMarkdown(th, cell); headRow.append(th); }
      head.append(headRow); table.append(head); const body = element('tbody');
      for (const row of block.rows) { const tr = element('tr'); for (let index = 0; index < block.header.length; index++) { const td = element('td'); inlineMarkdown(td, row[index] || ''); tr.append(td); } body.append(tr); }
      table.append(body); wrap.append(table); fragment.append(wrap); continue;
    }
    const node = element(block.type === 'heading' ? `h${Math.min(6, block.level + 1)}` : block.type === 'quote' ? 'blockquote' : 'p');
    inlineMarkdown(node, block.text); fragment.append(node);
  }
  container.replaceChildren(fragment);
}

function element(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined && text !== null) node.textContent = String(text);
  return node;
}

function notice(message, kind = 'neutral') {
  const node = $('global-notice');
  node.textContent = message;
  node.className = `notice ${kind}`;
  node.hidden = !message;
}

function showConnection(show = true) {
  if (show) { selectTab('options'); $('api-token').focus(); }
  else selectTab('chat');
}

function setBadge(node, status) {
  node.textContent = String(status || 'unknown').toUpperCase();
  node.className = `badge ${classifyStatus(status)}`;
}

function renderGenerationVisibility() {
  const view = generationPanelState(state.activeTab, state.preferences.show_advanced);
  $('generation-settings').hidden = !view.visible;
  $('generation-chat-controls').hidden = !view.fullControls;
  $('reasoning-request-label').hidden = view.workspace;
  $('reasoning-workspace-label').hidden = !view.workspace;
  $('generation-workspace-note').hidden = !view.workspace;
}

function selectTab(name, focus = false) {
  if (!$(`tab-${name}`) || $(`tab-${name}`).hidden) name = 'chat';
  state.activeTab = name;
  for (const tab of document.querySelectorAll('[data-tab]')) {
    const active = tab.dataset.tab === name;
    tab.classList.toggle('active', active);
    tab.setAttribute('aria-selected', String(active));
    tab.tabIndex = active ? 0 : -1;
    if (active && focus) tab.focus();
    $(`panel-${tab.dataset.tab}`).hidden = !active;
  }
  const label = $(`tab-${name}`)?.querySelector('.nav-label')?.textContent || name;
  $('view-label').textContent = label;
  renderGenerationVisibility();
  if (state.authenticated && name === 'models') refreshCatalog();
  if (state.authenticated && name === 'benchmarks') refreshBenchmarks();
  if (state.authenticated && name === 'workspace') refreshWorkspaces();
  if (state.authenticated && name === 'options') refreshSettings();
}

function renderSettings(settings) {
  const api = apiSettings(settings?.api);
  state.settings = { ...settings, api };
  for (const [key, enabled] of Object.entries(api)) $(`api-${key.replaceAll('_', '-')}`).checked = enabled;
  $('api-settings-fields').disabled = state.settingsBusy || !state.authenticated;
  $('rotate-api-token').disabled = state.settingsBusy || !state.authenticated || settings.token_rotation_supported !== true;
  $('settings-listen').textContent = settings.listen || 'Not reported';
  $('settings-backend').textContent = settings.backend || 'Not reported';
  $('settings-model').textContent = settings.model || state.model || 'Not reported';
  $('settings-api-note').textContent = settings.api_note || 'Disabling a category blocks new requests. Existing status, read, cancel and close controls remain available.';
  $('settings-network-note').textContent = settings.network_note || 'Network binding and backend are managed by the server configuration. Changing them requires a gateway restart, not a rank restart.';
}

async function refreshSettings() {
  if (!state.authenticated || state.settingsBusy) return;
  state.settingsBusy = true;
  $('refresh-settings').disabled = true;
  $('api-settings-fields').disabled = true;
  $('rotate-api-token').disabled = true;
  try { renderSettings(await request('/v1/settings')); $('settings-result').textContent = state.language === 'it' ? 'Impostazioni server caricate.' : 'Server settings loaded.'; }
  catch (error) { if (error.code !== 'stale_credential') { state.settings = null; $('settings-result').textContent = error.message; } }
  finally {
    state.settingsBusy = false; $('refresh-settings').disabled = false;
    if (state.settings) renderSettings(state.settings);
  }
}

async function saveAPISettings(event) {
  event.preventDefault();
  if (!state.authenticated || !state.settings || state.settingsBusy) return;
  const api = Object.fromEntries(['chat', 'workspaces', 'legacy_coding', 'operations'].map(key => [key, $(`api-${key.replaceAll('_', '-')}`).checked]));
  const question = state.language === 'it' ? 'Salvare i controlli API per tutti i client? Bloccare nuove richieste non annulla quelle attive e non cambia la rete o il modello.' : 'Save API controls for all clients? Disabling new requests does not cancel active work or change the network or model.';
  if (!window.confirm(question)) return;
  state.settingsBusy = true; $('api-settings-fields').disabled = true; $('rotate-api-token').disabled = true;
  try { renderSettings(await request('/v1/settings', { method: 'PUT', body: { api: apiSettings(api) } })); $('settings-result').textContent = state.language === 'it' ? 'Controlli API salvati.' : 'API controls saved.'; }
  catch (error) { $('settings-result').textContent = `${error.message} ${state.language === 'it' ? 'Aggiorna per verificare lo stato; nessun retry automatico.' : 'Refresh to verify the state; no automatic retry.'}`; }
  finally { state.settingsBusy = false; if (state.settings) renderSettings(state.settings); }
}

async function rotateAPIToken() {
  if (!state.authenticated || state.settingsBusy || state.settings?.token_rotation_supported !== true) return;
  const question = state.language === 'it' ? 'Sostituire il token API del server? Il vecchio token smetterà di funzionare. Questa scheda userà il nuovo token; gli altri client devono riconnettersi.' : 'Replace the server API token? The old token will stop working. This tab will use the new token; other clients must reconnect.';
  if (!window.confirm(question)) return;
  state.settingsBusy = true; $('rotate-api-token').disabled = true; $('api-settings-fields').disabled = true;
  try {
    const result = await request('/v1/settings/token', { method: 'POST', body: { confirm: true } });
    if (typeof result?.token !== 'string' || !result.token) throw new Error('No new token was returned. Check the local server token file before retrying.');
    state.token = result.token; $('api-token').value = result.token;
    $('connection-result').textContent = state.language === 'it' ? 'Token ruotato. Questa scheda è connessa; copia il nuovo token per gli altri client. Non viene salvato nel browser.' : 'Token rotated. This tab is connected; copy the new token for other clients. It is not saved in the browser.';
  } catch (error) { $('connection-result').textContent = `${error.message} No automatic retry. If completion is uncertain, read the current token from the server's private state/api-token file.`; }
  finally { state.settingsBusy = false; if (state.settings) renderSettings(state.settings); }
}

function headers(token = state.token) {
  const result = { Accept: 'application/json' };
  if (token) result.Authorization = `Bearer ${token}`;
  return result;
}

async function request(path, options = {}) {
  const controller = new AbortController();
  // An old in-flight response must not invalidate a credential installed by
  // reconnect or rotation while this request was awaiting the server.
  const dispatchedToken = state.token;
  const timeout = setTimeout(() => controller.abort(), options.timeout || 15000);
  try {
    const response = await fetch(path, {
      method: options.method || 'GET', credentials: 'omit', cache: 'no-store',
      mode: 'same-origin', redirect: 'error', signal: controller.signal,
      headers: { ...headers(dispatchedToken), ...(options.body ? { 'Content-Type': 'application/json' } : {}) },
      body: options.body ? JSON.stringify(options.body) : undefined,
    });
    const payload = await response.json().catch(() => null);
    if (!response.ok) {
      if (response.status === 401 || (response.status === 403 && payload?.code !== 'api_disabled')) {
        if (dispatchedToken !== state.token) throw Object.assign(new Error('Authentication changed while this request was in flight. Refresh to inspect its current state.'), { code: 'stale_credential' });
        state.authenticated = false;
        $('api-settings-fields').disabled = true;
        $('rotate-api-token').disabled = true;
        $('connection-label').textContent = "Token required";
        $('connection-dot').className = 'status-dot bad';
        showConnection();
      }
      throw new Error(errorMessage(payload, `HTTP ${response.status}. Check the local API token and server state.`));
    }
    return payload;
  } catch (error) {
    if (error.name === 'AbortError') throw new Error('The status request timed out. The server may still be working.');
    throw error;
  } finally {
    clearTimeout(timeout);
  }
}

function renderOptions(options) {
  if (!Array.isArray(options?.reasoning_modes) || !Array.isArray(options?.context_options)) throw new Error('The server did not provide generation options.');
  const previous = state.options;
  const reasoning = previous ? $('reasoning-mode').value : options.default_reasoning;
  const context = previous ? Number($('context-select').value) : options.default_context_tokens;
  state.options = options;
  $('reasoning-mode').replaceChildren(...options.reasoning_modes.map(mode => { const option = element('option', '', mode); option.value = mode; return option; }));
  $('reasoning-mode').value = options.reasoning_modes.includes(reasoning) ? reasoning : options.default_reasoning;
  state.reasoning = $('reasoning-mode').value;
  $('context-select').replaceChildren(...options.context_options.map(value => { const option = element('option', '', `${number(value, 0)} token`); option.value = value; return option; }));
  $('context-select').value = options.context_options.includes(context) ? context : options.default_context_tokens;
  for (const id of ['reasoning-mode', 'context-select', 'chat-cap']) $(id).disabled = false;
  for (const option of $('chat-cap').options) option.disabled = option.value !== '' && Number(option.value) > options.max_output_tokens;
  if ($('chat-cap').selectedOptions[0]?.disabled) $('chat-cap').value = '';
  $('thinking-budget').disabled = options.thinking_budget_supported !== true;
  const timeoutLimit = Number(options.generation_timeout_seconds);
  if (timeoutLimit > 0) { $('code-timeout').max = timeoutLimit; $('code-timeout').value = Math.min(Number($('code-timeout').value), timeoutLimit); }
  if ($('thinking-budget').disabled) $('thinking-budget').value = '';
  $('generation-limits').textContent = `Auto ≤ ${number(options.default_max_tokens, 0)} available tokens. Backend timeout ${number(options.generation_timeout_seconds, 0)} s. Cap/timeout: incomplete answer.`;
  $('profile-note').textContent = "Selectable context is not a quality qualification.";
  $('profile-note').title = options.quality_note || "A selectable context does not imply quality has been verified at that length.";
}

function selectedSettings() {
  const settings = generationSettings($('reasoning-mode').value, $('context-select').value, $('chat-cap').value, state.options);
  if (!$('thinking-budget').disabled && $('thinking-budget').value !== '') settings.thinking_token_budget = Number($('thinking-budget').value);
  return settings;
}

function memoryValues(node) {
  return {
    used: finite(node.memory_used_bytes ?? node.memory?.used_bytes),
    total: finite(node.memory_total_bytes ?? node.memory?.total_bytes),
  };
}

function nodeCard(node, index) {
  const card = element('article', 'surface');
  const title = element('div', 'node-title');
  const name = element('div');
  name.append(element('h3', '', node.name || `NODE ${index + 1}`));
  name.append(element('p', 'node-address', node.address || node.host || 'Address not reported'));
  const badge = element('span');
  const nodeHealth = typeof node.health === 'object' ? node.health?.status : node.health;
  setBadge(badge, nodeHealth || node.status || 'unknown');
  title.append(name, badge);
  card.append(title);
  const memory = memoryValues(node);
  const memoryRow = element('div', 'node-detail');
  memoryRow.append(element('span', '', 'UMA memory'), element('strong', '', memory.total === null ? bytes(memory.used) : `${bytes(memory.used)} / ${bytes(memory.total)}`));
  const memoryMeter = element('div', 'meter');
  const memoryFill = element('span');
  if (memory.used !== null && memory.total > 0) memoryFill.style.width = `${Math.max(0, Math.min(100, memory.used / memory.total * 100))}%`;
  memoryMeter.append(memoryFill);
  const gpu = finite(node.gpu_utilization_percent ?? node.gpu?.utilization_percent);
  const gpuRow = element('div', 'node-detail');
  gpuRow.append(element('span', '', 'GPU utilization'), element('strong', '', gpu === null ? '—' : `${number(gpu, 1)}%`));
  const gpuMeter = element('div', 'meter gpu');
  const gpuFill = element('span');
  if (gpu !== null) gpuFill.style.width = `${Math.max(0, Math.min(100, gpu))}%`;
  gpuMeter.append(gpuFill);
  card.append(memoryRow, memoryMeter, gpuRow, gpuMeter);
  if (node.detail || node.error) card.append(element('p', 'small muted', String(node.detail || node.error)));
  return card;
}

function renderHealth(health, status) {
  const healthState = healthStatus(status?.health, healthStatus(health));
  setBadge($('cluster-health'), healthState);
  $('cluster-model').textContent = status?.model || state.model || 'Model not reported';
  $('model-pill').textContent = status?.model || state.model || 'Server connected';
  const runtime = status?.runtime;
  $('cluster-runtime').textContent = typeof runtime === 'string' ? runtime
    : (runtime && typeof runtime === 'object' ? JSON.stringify(runtime) : 'Runtime details not reported by the server.');
  const nodes = Array.isArray(status?.nodes) ? status.nodes : [];
  $('cluster-nodes').replaceChildren(...(nodes.length ? nodes.map(nodeCard)
    : [element('div', 'surface missing-state', 'The server has not reported per-node telemetry. No utilization values are inferred.')]));
  $('cluster-acceptance').textContent = percent(status?.acceptance ?? status?.dflash_acceptance);
  $('cluster-active').textContent = activeRequestLabel(status?.active_request, status?.health?.busy ?? health?.busy);
  state.contextLimit = finite(status?.context_limit ?? status?.max_model_len);
  $('cluster-context').textContent = number(state.contextLimit, 0);
  $('cluster-updated').textContent = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  $('cluster-check-note').textContent = 'Browser receipt time · live API data';
  $('cluster-json').textContent = JSON.stringify({ health, status }, null, 2);
}

async function refreshHealth(interactive = false) {
  if (state.healthBusy) return;
  state.healthBusy = true;
  $('refresh-health').disabled = true;
  $('refresh-cluster').disabled = true;
  try {
    const health = await request('/health');
    const [status, models, options] = await Promise.all([request('/v1/status'), request('/v1/models'), request('/v1/options')]);
    renderOptions(options);
    state.authenticated = true;
    state.model = status?.model || models?.data?.[0]?.id || '';
    renderHealth(health, status);
    const healthState = healthStatus(status?.health, healthStatus(health));
    const bad = classifyStatus(healthState) === 'bad';
    $('connection-label').textContent = bad ? "Engine needs attention" : "Local API connected";
    $('connection-dot').className = `status-dot ${bad ? 'bad' : 'good'}`;
    $('connection-result').textContent = bad ? "API reachable; check cluster health before generating." : "Authenticated: chat and workspace controls are available.";
    if (interactive) notice(bad ? 'The API is reachable, but the engine reports an unhealthy state.' : '', bad ? 'bad' : 'neutral');
  } catch (error) {
    if (error.code === 'stale_credential') return;
    state.authenticated = false;
    $('connection-dot').className = 'status-dot bad';
    if (!$('connection-label').textContent.includes('Token')) $('connection-label').textContent = "Connection unavailable";
    $('model-pill').textContent = "API unavailable";
    $('connection-result').textContent = error.message;
    $('cluster-check-note').textContent = 'Latest check failed; previously displayed telemetry is stale.';
    setBadge($('cluster-health'), 'unknown');
    if (interactive) notice(error.message, 'bad');
  } finally {
    state.healthBusy = false;
    $('refresh-health').disabled = false;
    $('refresh-cluster').disabled = false;
  }
}

function message(role, content = '') {
  $('chat-empty').hidden = true;
  const outer = element('article', `message ${role}`);
  const avatar = element('span', 'message-avatar', role === 'assistant' ? 'AI' : "YOU");
  const body = element('div');
  const meta = element('div', 'message-meta', role === 'assistant' ? `reasoning ${state.reasoning} · ${state.model || 'Assistant'}` : "YOU");
  const text = element('div', 'message-content', content);
  const details = element('details', 'reasoning-details');
  const summary = element('summary', '', 'Reasoning');
  const reasoning = element('pre');
  details.append(summary, reasoning);
  details.hidden = true;
  details.open = $('show-thinking').checked;
  const error = element('p', 'message-error');
  error.hidden = true;
  body.append(meta, details, text, error);
  outer.append(avatar, body);
  $('conversation').append(outer);
  const record = { role, content, created_utc: new Date().toISOString(), status: role === 'user' ? 'submitted' : 'pending' };
  state.records.push(record);
  return { outer, meta, text, details, reasoning, error, record };
}

function scrollChat() {
  const container = $('conversation');
  if (container.scrollHeight - container.scrollTop - container.clientHeight < 300) container.scrollTop = container.scrollHeight;
}

function updateChatMetrics(usage, timings, firstTokenMS, started, finished = false, admitted = null) {
  const live = observedRate(usage, (performance.now() - started) / 1000);
  $('chat-live-tps').textContent = live === null ? '—' : `${number(live, 2)} tok/s`;
  const decode = decodeRate(timings, usage);
  $('chat-tps').textContent = finite(decode) === null ? '—' : `${number(decode, 2)} tok/s`;
  $('chat-tps').title = 'Server decode TPS, or (completion tokens − 1) / server generation time. Never HTTP tokens/second.';
  $('chat-ttft').textContent = firstTokenMS === null ? '—' : seconds(firstTokenMS / 1000);
  $('chat-ttft').title = 'Browser-observed first content or reasoning token; includes network transit.';
  $('chat-wall').textContent = seconds((performance.now() - started) / 1000) + (finished ? '' : ' …');
  $('chat-wall').title = 'Browser-observed HTTP wall time, not engine decode time.';
  const completion = usage?.completion_tokens;
  const reasoning = usage?.completion_tokens_details?.reasoning_tokens ?? usage?.reasoning_tokens;
  $('chat-tokens').textContent = finite(completion) === null ? '—'
    : `${number(completion, 0)}${finite(reasoning) === null ? '' : ` (${number(reasoning, 0)} reasoning)`}`;
  const prompt = finite(usage?.prompt_tokens ?? admitted?.prompt);
  $('chat-context').textContent = prompt === null ? '—'
    : `${number(prompt, 0)}${admitted?.context ? ` / ${number(admitted.context, 0)}` : ''}`;
  $('chat-context').title = 'Server-reported prompt tokens. This is not a local token estimate.';
}

async function sendChat(event) {
  event.preventDefault();
  if (state.chatController) return;
  if (state.uploading) { notice('Wait for attachment extraction before sending.'); return; }
  const input = $('chat-input').value.trim();
  if (!input) return;
  if (!state.authenticated || !state.model) { showConnection(); notice('Connect the local API before sending a message.'); return; }
  let settings;
  try { settings = selectedSettings(); } catch (error) { notice(error.message, 'bad'); return; }
  notice('');
  const requestReasoning = settings.reasoning_effort;
  state.reasoning = requestReasoning;
  const userMessage = { role: 'user', content: input };
  if (state.attachments.length) userMessage.attachment_ids = state.attachments.map(attachment => attachment.id);
  const user = message('user', input);
  user.record.attachment_ids = userMessage.attachment_ids || [];
  user.record.attachments = state.attachments.map(({ id, name, kind, size_bytes, sha256, truncated, warning }) => ({ id, name, kind, size_bytes, sha256, truncated, warning }));
  if (state.attachments.length) user.meta.append(element('span', '', ` · ${state.attachments.map(item => item.name).join(', ')}`));
  state.attachments = []; renderAttachments();
  const output = message('assistant');
  output.record.settings = settings;
  output.meta.textContent += " · connecting";
  $('chat-input').value = '';
  const controller = new AbortController();
  state.chatController = controller;
  $('send-chat').disabled = true;
  $('clear-chat').disabled = true;
  $('stop-chat').hidden = false;
  const started = performance.now();
  let text = '', reasoning = '', finish = null, usage = null, timings = null;
  let firstTokenMS = null, done = false, protocolError = null, admitted = null, lastRender = 0;
  const ticker = setInterval(() => updateChatMetrics(usage, timings, firstTokenMS, started, false, admitted), 500);
  function receive(payload) {
    if (payload?.error) { protocolError = errorMessage(payload); return; }
    const delta = completionDelta(payload);
    if ((delta.content || delta.reasoning) && firstTokenMS === null) firstTokenMS = performance.now() - started;
    text += delta.content;
    reasoning += delta.reasoning;
    if (delta.finish) finish = delta.finish;
    if (delta.usage) usage = delta.usage;
    if (delta.timings) timings = delta.timings;
    if (performance.now() - lastRender > 120) { renderMarkdown(output.text, text); lastRender = performance.now(); }
    output.reasoning.textContent = reasoning;
    output.details.hidden = !reasoning;
    output.meta.textContent = `reasoning ${requestReasoning} · ${finish || 'streaming'}`;
    updateChatMetrics(usage, timings, firstTokenMS, started, false, admitted);
    scrollChat();
  }
  try {
    const response = await fetch('/v1/chat/completions', {
      method: 'POST', credentials: 'omit', cache: 'no-store', mode: 'same-origin', redirect: 'error',
      signal: controller.signal,
      headers: { ...headers(), Accept: 'text/event-stream', 'Content-Type': 'application/json' },
      body: JSON.stringify({ model: state.model, ...settings, messages: [...state.chat, userMessage], stream: true, stream_options: { include_usage: true, continuous_usage_stats: true } }),
    });
    if (!response.ok) {
      const failure = await response.json().catch(() => null);
      throw new Error(errorMessage(failure, `HTTP ${response.status}`));
    }
    admitted = { prompt: finite(response.headers.get('X-StrixGLM-Prompt-Tokens')), context: finite(response.headers.get('X-StrixGLM-Context-Tokens')), maximum: finite(response.headers.get('X-StrixGLM-Max-Tokens')) };
    if (!(response.headers.get('content-type') || '').includes('text/event-stream')) {
      receive(await response.json());
      done = true;
    } else {
      if (!response.body) throw new Error('This browser did not provide a readable response stream.');
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      const parser = new SSEParser(event => {
        if (event.data.trim() === '[DONE]') { done = true; return; }
        if (event.event === 'error') { protocolError = errorMessage(event.data); return; }
        try { receive(JSON.parse(event.data)); }
        catch { protocolError = 'Malformed JSON in the server event stream.'; }
      });
      for (;;) {
        const result = await reader.read();
        if (result.done) break;
        parser.push(decoder.decode(result.value, { stream: true }));
        if (protocolError) throw new Error(protocolError);
      }
      parser.push(decoder.decode());
      parser.finish();
    }
    if (protocolError) throw new Error(protocolError);
    const resultState = completionState(done, finish, text);
    if (resultState === 'incomplete-stream') throw new Error('The stream ended without a completion marker ([DONE]). Partial output was not added to history.');
    if (resultState !== 'complete') {
      output.error.hidden = false;
      output.error.textContent = finish === 'length' ? 'INCOMPLETE: output cap reached. This answer was not added to conversation history.'
        : `Not a completed text answer (${finish || 'no finish reason'}). This output was not added to history.`;
    } else {
      state.chat.push(userMessage, { role: 'assistant', content: text });
    }
    output.record.status = resultState;
    output.meta.textContent = `reasoning ${requestReasoning} · ${resultState === 'complete' ? "complete" : "incomplete"}${admitted.maximum ? ` · max ${number(admitted.maximum, 0)}` : ''}`;
  } catch (error) {
    output.record.status = error.name === 'AbortError' ? 'interrupted' : 'error';
    output.record.error = error.message;
    output.error.hidden = false;
    output.error.textContent = error.name === 'AbortError'
      ? 'Stream stopped. Partial output was not added to history; the engine may be draining the request.' : error.message;
    output.meta.textContent = `reasoning ${requestReasoning} · ${error.name === 'AbortError' ? "interrupted" : "error"}`;
  } finally {
    clearInterval(ticker);
    state.chatController = null;
    output.record.content = text;
    output.record.reasoning = reasoning;
    output.record.finish_reason = finish;
    output.record.metrics = { usage, timings, browser_ttft_ms: firstTokenMS, http_seconds: (performance.now() - started) / 1000, admitted };
    renderMarkdown(output.text, text);
    updateChatMetrics(usage, timings, firstTokenMS, started, true, admitted);
    $('send-chat').disabled = false;
    $('clear-chat').disabled = false;
    $('stop-chat').hidden = true;
    $('chat-input').focus();
    saveConversation(input);
  }
}

function renderDiff(diff) {
  const container = $('task-diff');
  if (!diff) { container.textContent = 'No patch has been produced yet.'; return; }
  const fragment = document.createDocumentFragment();
  for (const line of diff.split('\n')) {
    const kind = line.startsWith('+++') || line.startsWith('---') || line.startsWith('@@') || line.startsWith('diff ')
      ? 'diff-header' : line.startsWith('+') ? 'diff-added' : line.startsWith('-') ? 'diff-removed' : '';
    fragment.append(element('span', `diff-line ${kind}`, line));
  }
  container.replaceChildren(fragment);
}

function attemptCard(attempt, index) {
  const details = element('details', 'attempt');
  const summary = element('summary');
  const heading = element('span', '', `Attempt ${attempt.index ?? index + 1} · reasoning ${attempt.reasoning_effort || attempt.reasoning || "not reported"}`);
  const badge = element('span');
  setBadge(badge, attempt.status || 'unknown');
  summary.append(heading, badge);
  const lines = [];
  for (const [label, phase] of [['BUILD', attempt.build], ['TESTS', attempt.tests]]) {
    if (!phase) { lines.push(`${label}: not reported`); continue; }
    lines.push(`${label}: ${phase.passed === true ? 'PASS' : phase.passed === false ? 'FAIL' : phase.status || 'unknown'}${phase.seconds === undefined ? '' : ` · ${seconds(phase.seconds)}`}`);
    if (phase.command) lines.push(`command: ${Array.isArray(phase.command) ? phase.command.join(' ') : phase.command}`);
    if (phase.output) lines.push(String(phase.output));
    if (phase.error) lines.push(String(phase.error));
    lines.push('');
  }
  if (attempt.error) lines.push(`ERROR: ${typeof attempt.error === 'string' ? attempt.error : JSON.stringify(attempt.error)}`);
  if (attempt.metrics) lines.push(`MEASURED METRICS\n${JSON.stringify(attempt.metrics, null, 2)}`);
  if (attempt.feedback) lines.push(`REPAIR FEEDBACK\n${attempt.feedback}`);
  details.append(summary, element('pre', '', lines.join('\n')));
  return details;
}

function renderTask(payload) {
  const previous = state.task;
  const task = normalizeTask(payload);
  state.task = task;
  state.taskID = task.id || state.taskID;
  $('task-id').textContent = state.taskID || 'Task ID not returned';
  $('resume-id').value = state.taskID;
  setBadge($('task-status'), task.status);
  const metrics = task.metrics || {};
  $('task-wall').textContent = seconds(task.wall_seconds ?? metrics.wall_seconds ?? metrics.http_seconds);
  $('task-calls').textContent = number(task.model_calls ?? metrics.model_calls, 0);
  const taskDecode = finite(metrics.decode_tps);
  const taskObserved = observedRate(metrics, metrics.http_seconds);
  $('task-tps').textContent = number(taskDecode ?? taskObserved, 2);
  $('task-tps-label').textContent = taskDecode === null ? "Observed HTTP TPS" : "Last attempt decode TPS";
  $('task-tps').title = "Last attempt: engine decode when available, otherwise real tokens / HTTP time. Not aggregate task throughput.";
  $('task-profile').textContent = task.reasoning_effort || task.attempts.at(-1)?.reasoning || '—';
  const terminal = isTerminal(task.status);
  const passed = isSuccess(task.status);
  $('task-summary').textContent = task.error ? errorMessage(task, typeof task.error === 'string' ? task.error : JSON.stringify(task.error))
    : String(task.status).toLowerCase() === 'applying' ? 'Applying the verified patch to the original repository. Waiting for the transaction and apply receipt; cancellation is unavailable during this step.'
      : task.final_response || (passed ? 'Tests passed. Review the diff before applying it to the original repository.'
      : terminal ? 'Task ended without a passing result. Candidate changes were not applied.'
        : 'Working in the isolated workspace. Build, test and repair results appear below.');
  const open = new Set([...$('attempts').querySelectorAll('details')].flatMap((node, index) => node.open ? [index] : []));
  const cards = task.attempts.map(attemptCard);
  cards.forEach((card, index) => { card.open = open.has(index); });
  $('attempts').replaceChildren(...cards);
  const sameDiff = previous?.id === task.id && previous?.diff === task.diff;
  if (!sameDiff) renderDiff(task.diff);
  $('files-changed').textContent = task.files_changed.length ? task.files_changed.join(' · ') : 'No changed files reported.';
  $('copy-diff').disabled = !task.diff;
  $('download-diff').disabled = !task.diff;
  $('cancel-task').disabled = !canCancelTask(task.status);
  $('refresh-task').disabled = !state.taskID;
  $('apply-task').disabled = !passed || !task.diff || task.applied === true || task.status === 'applied';
  $('start-task').disabled = !terminal;
  $('task-json').textContent = JSON.stringify(payload, null, 2);
  return task;
}

function scheduleTaskPoll(id, generation) {
  clearTimeout(state.taskTimer);
  if (!id || generation !== state.taskGeneration || isTerminal(state.task?.status)) return;
  state.taskTimer = setTimeout(() => pollTask(id, generation), 1500);
}

async function pollTask(id = state.taskID, generation = state.taskGeneration) {
  if (!id) return;
  try {
    const result = await request(`/v1/coding/tasks/${encodeURIComponent(id)}`);
    if (generation !== state.taskGeneration || id !== state.taskID) return;
    const task = renderTask(result);
    $('task-action-result').textContent = '';
    if (!isTerminal(task.status)) scheduleTaskPoll(id, generation);
  } catch (error) {
    if (generation !== state.taskGeneration) return;
    $('task-action-result').textContent = `Status unavailable: ${error.message} Use Refresh to resume monitoring; no new task will be submitted.`;
    clearTimeout(state.taskTimer);
    $('refresh-task').disabled = false;
  }
}

async function startTask(event) {
  event.preventDefault();
  if (!state.authenticated) { showConnection(); notice('Connect the local API before starting a coding task.'); return; }
  if (state.task && !isTerminal(state.task.status)) { notice('Wait for or cancel the current task before starting another.'); return; }
  let settings;
  try { settings = selectedSettings(); } catch (error) { notice(error.message, 'bad'); return; }
  const spec = {
    ...state.importedOptions,
    ...settings,
    task: $('code-task').value.trim(), repo: $('code-repo').value.trim(),
    allowed_paths: pathList($('code-paths').value), test_command: $('code-test').value.trim(),
    build_command: $('code-build').value.trim(), timeout: Number($('code-timeout').value),
    max_repairs: Number($('code-repairs').value),
    sandbox_policy: 'isolated', apply: false,
  };
  if (!spec.task || !spec.repo.startsWith('/') || !spec.allowed_paths.length || !spec.test_command) {
    $('coding-error').textContent = 'Provide an instruction, absolute repository path, allowed source paths and a test command.';
    $('coding-error').hidden = false;
    return;
  }
  if (!Number.isSafeInteger(spec.timeout) || spec.timeout < 10 || spec.timeout > Number($('code-timeout').max) || !Number.isSafeInteger(spec.max_repairs)) { notice("Timeout or repair count exceeds server limits.", 'bad'); return; }
  $('coding-error').hidden = true;
  $('start-task').disabled = true;
  $('task-action-result').textContent = 'Submitting one task…';
  try {
    const result = await request('/v1/coding/tasks', { method: 'POST', body: spec, timeout: 30000 });
    if (!result?.id && !result?.task_id) throw new Error('The server did not return a task ID. Check server state before resubmitting.');
    state.taskID = result.id || result.task_id;
    state.taskGeneration++;
    clearTimeout(state.taskTimer);
    renderTask(result);
    await pollTask(state.taskID, state.taskGeneration);
  } catch (error) {
    $('coding-error').textContent = `${error.message} No automatic retry was made.`;
    $('coding-error').hidden = false;
    $('start-task').disabled = false;
    $('task-action-result').textContent = '';
  }
}

async function taskAction(action) {
  if (!state.taskID) return;
  if (action === 'apply' && !window.confirm('Apply this passing patch to the ORIGINAL repository? Review the diff first. The server must refuse changed source files or non-passing results.')) return;
  const id = state.taskID;
  $(action === 'apply' ? 'apply-task' : 'cancel-task').disabled = true;
  try {
    const result = await request(`/v1/coding/tasks/${encodeURIComponent(id)}/${action}`, {
      method: 'POST', body: action === 'apply' ? { confirm: true } : {}, timeout: 30000,
    });
    $('task-action-result').textContent = action === 'apply' ? 'Apply request accepted. Refreshing the recorded result.' : 'Cancellation requested. Waiting for the server to finish cancellation / draining.';
    if (result?.id) renderTask(result);
    await pollTask(id, state.taskGeneration);
  } catch (error) {
    $('task-action-result').textContent = error.message;
    if (state.task) renderTask(state.task);
  }
}

function saveConversation(title = '') {
  const existing = state.conversations.find(item => item.id === state.conversationID);
  const item = existing || { id: ++state.conversationID, title: title.slice(0, 50) || "Conversation" };
  item.chat = state.chat;
  item.records = state.records;
  item.nodes = [...$('conversation').querySelectorAll('.message')];
  item.metrics = Object.fromEntries(['chat-live-tps', 'chat-tps', 'chat-ttft', 'chat-wall', 'chat-tokens', 'chat-context'].map(id => [id, $(id).textContent]));
  if (!existing) state.conversations.push(item);
  renderConversations();
}

function renderConversations() {
  $('conversation-list').replaceChildren(...state.conversations.map(item => {
    const button = element('button', item.id === state.conversationID ? 'conversation-item active' : 'conversation-item', item.title);
    button.type = 'button';
    button.addEventListener('click', () => {
      if (state.chatController) { notice("Wait for or stop the stream before switching conversations."); return; }
      state.conversationID = item.id;
      state.chat = item.chat;
      state.records = item.records;
      $('conversation').querySelectorAll('.message').forEach(node => node.remove());
      $('conversation').append(...item.nodes);
      $('chat-empty').hidden = true;
      for (const [id, value] of Object.entries(item.metrics)) $(id).textContent = value;
      renderConversations();
      selectTab('chat');
    });
    return button;
  }));
}

function setSidebar(collapsed) {
  document.body.classList.toggle('sidebar-collapsed', collapsed);
  $('ui-sidebar-collapsed').checked = collapsed;
  for (const id of ['sidebar-toggle', 'sidebar-collapse']) $(id).setAttribute('aria-expanded', String(!collapsed));
}

function renderAttachments() {
  if (!state.attachments.length && !state.uploading) $('attachment-status').textContent = 'No pending attachments. Uploading does not run inference.';
  $('attachment-list').replaceChildren(...state.attachments.map(attachment => {
    const card = element('details', 'attachment');
    card.append(element('summary', '', `${attachment.name} · ${attachment.kind} · ${bytes(attachment.size_bytes)}`));
    if (attachment.truncated || attachment.warning) card.append(element('p', 'notice', `${attachment.truncated ? 'Extraction truncated. ' : ''}${attachment.warning || ''}`));
    card.append(element('pre', '', String(attachment.text || '').slice(0, 16000)));
    if ((attachment.text || '').length > 16000) card.append(element('p', 'small muted', 'Preview shortened; the server retains the full bounded extraction.'));
    const remove = element('button', 'button quiet small-button', state.language === 'it' ? 'Rimuovi' : 'Remove'); remove.type = 'button';
    remove.addEventListener('click', () => { state.attachments = state.attachments.filter(item => item.id !== attachment.id); renderAttachments(); });
    card.append(remove); return card;
  }));
}

async function uploadAttachments(event) {
  const files = [...event.target.files]; event.target.value = '';
  if (!state.authenticated) { showConnection(); notice('Connect before uploading files.'); return; }
  if (state.attachments.length + files.length > 8) { notice('A message accepts up to eight attachments.', 'bad'); return; }
  if (files.some(file => file.size > 32 * 1024 * 1024)) { notice('Each attachment must be at most 32 MiB.', 'bad'); return; }
  state.uploading++;
  $('chat-attachments').disabled = true;
  try {
    for (const file of files) {
      $('attachment-status').textContent = `Extracting ${file.name}… Uploading does not run inference.`;
      const form = new FormData(); form.append('file', file);
      const response = await fetch('/v1/attachments', { method: 'POST', body: form, headers: headers(), credentials: 'omit', redirect: 'error', mode: 'same-origin', signal: AbortSignal.timeout(120000) });
      const attachment = await response.json();
      if (!response.ok) throw new Error(errorMessage(attachment, `HTTP ${response.status}`));
      if (!attachment?.id || typeof attachment.text !== 'string') throw new Error('Invalid attachment extraction response.');
      state.attachments.push(attachment); renderAttachments();
    }
    $('attachment-status').textContent = `${state.attachments.length} attachment(s) ready. Text extraction only; no image/audio understanding.`;
  } catch (error) { $('attachment-status').textContent = `${error.message} No automatic upload retry.`; }
  finally { state.uploading--; $('chat-attachments').disabled = false; }
}

function downloadJSON(value, name) {
  const url = URL.createObjectURL(new Blob([JSON.stringify(value, null, 2)], { type: 'application/json;charset=utf-8' }));
  const link = element('a'); link.href = url; link.download = name; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
}

function workspaceState() { return String(state.workspace?.state || state.workspace?.status || '').toUpperCase(); }

function renderWorkspace() {
  const options = state.workspaceOptions, pi = options?.pi || {};
  const current = workspaceState();
  $('workspace-capability').textContent = !options ? 'Workspace API unavailable. No actions have been started.' : `${pi.installed ? `Pi ${pi.version || 'installed'}` : 'Pi not installed'} · ${pi.tools_available ? 'Agent tools available' : pi.blocked_reason || 'Agent tools not qualified for this backend'}`;
  const roots = (options?.local_roots || []).map(root => typeof root === 'string' ? root : root.path || root.root);
  $('workspace-roots').textContent = `Allowed local roots: ${roots.join(' · ') || 'not reported'}`;
  const commandList = options?.terminal?.commands || [];
  $('workspace-command').replaceChildren(...commandList.map(command => { const id = typeof command === 'string' ? command : command.id; const option = element('option', '', typeof command === 'string' ? command : command.label || id); option.value = id; return option; }));
  const connected = ['CONNECTED', 'READY', 'RUNNING', 'ABORTING'].includes(current);
  $('workspace-create').disabled = !options || state.workspaceBusy;
  $('workspace-connect').disabled = !state.workspace || !['CREATED', 'FAILED'].includes(current) || state.workspaceBusy;
  $('workspace-start').disabled = !pi.tools_available || state.workspace?.capabilities?.pi !== true || current !== 'CONNECTED' || state.workspaceBusy;
  $('workspace-send').disabled = !pi.tools_available || state.workspace?.capabilities?.prompt !== true || current !== 'READY' || state.workspaceBusy;
  $('workspace-abort').disabled = !['RUNNING', 'STARTING'].includes(current) || state.workspaceBusy;
  $('workspace-close').disabled = !state.workspace || current === 'CLOSED' || state.workspaceBusy;
  $('workspace-files-button').disabled = !connected || state.workspace?.capabilities?.files !== true || state.workspaceBusy;
  for (const id of ['workspace-terminal-run', 'workspace-command']) $(id).disabled = !connected || state.workspace?.capabilities?.terminal !== true || state.workspaceBusy;
  for (const id of ['workspace-shell-run', 'workspace-shell-command']) $(id).disabled = !canRunWorkspaceShell(state.workspace) || state.workspaceBusy;
  $('workspace-status').textContent = state.workspace ? `${state.workspace.id} · ${current} · ${state.workspace.root || ''}${state.workspace.error || state.workspace.blocked_reason ? ` · ${state.workspace.error || state.workspace.blocked_reason}` : ''}` : 'No active workspace.';
}

async function refreshWorkspaces() {
  try {
    const [options, sessions, presets] = await Promise.all([request('/v1/workspaces/options'), request('/v1/workspaces/sessions'), request('/v1/workspaces/presets')]);
    state.workspaceOptions = options; state.workspaceSessions = sessions?.sessions || [];
    const selected = state.workspace?.id || '';
    $('workspace-session').replaceChildren(element('option', '', 'Choose a session'), ...state.workspaceSessions.map(session => { const option = element('option', '', `${session.id} · ${session.kind} · ${session.state || session.status} · ${session.root}`); option.value = session.id; return option; }));
    $('workspace-session').options[0].value = ''; $('workspace-session').value = selected;
    const selectedPreset = $('workspace-preset').value;
    $('workspace-preset').replaceChildren(element('option', '', 'Choose a preset'), ...(presets?.presets || []).map(preset => { const option = element('option', '', `${preset.name} · ${preset.user}@${preset.host}:${preset.root}`); option.value = preset.id; option.dataset.root = preset.root; return option; }));
    $('workspace-preset').options[0].value = ''; $('workspace-preset').value = selectedPreset;
    if (selected) state.workspace = state.workspaceSessions.find(session => session.id === selected) || state.workspace;
    renderWorkspace();
  } catch (error) { state.workspaceOptions = null; renderWorkspace(); $('workspace-capability').textContent = `Workspace API unavailable: ${error.message} Advanced / legacy coding remains separate.`; }
}

async function workspaceAction(action, body = {}) {
  if (!state.workspace?.id || state.workspaceBusy) return;
  const id = state.workspace.id;
  if (!window.confirm(`${action.toUpperCase()} in workspace ${state.workspace.root || id}?${action === 'prompt' ? ' Pi may read, write and execute tools in this workspace.' : ''}`)) return;
  state.workspaceBusy = true; renderWorkspace();
  try {
    const result = await request(`/v1/workspaces/sessions/${encodeURIComponent(id)}/${action}`, { method: 'POST', body: { ...body, confirm: true }, timeout: 60000 });
    if (result?.id) state.workspace = result;
    if (action === 'prompt') { $('workspace-messages').append(element('p', 'pi-user-message', body.message)); $('workspace-prompt').value = ''; }
    await pollWorkspace();
  } catch (error) { notice(`${error.message} No automatic retry. Refresh the workspace before resubmitting.`, 'bad'); }
  finally { state.workspaceBusy = false; $('workspace-password').value = ''; renderWorkspace(); }
}

async function pollWorkspace() {
  clearTimeout(state.workspaceTimer);
  const id = state.workspace?.id;
  if (!id) return;
  try {
    const [session, payload] = await Promise.all([request(`/v1/workspaces/sessions/${encodeURIComponent(id)}`), request(`/v1/workspaces/sessions/${encodeURIComponent(id)}/events?after=${state.workspaceSequence}`)]);
    if (state.workspace?.id !== id) return;
    state.workspace = session;
    for (const item of payload.events || []) {
      state.workspaceEvents.push(item);
      const event = item.event || {};
      if (event.type === 'message_update' && event.assistantMessageEvent?.type === 'text_delta') {
        if (!state.workspaceAssistant) { state.workspaceAssistant = { node: element('div', 'message-content pi-assistant-message'), text: '' }; $('workspace-messages').append(state.workspaceAssistant.node); }
        state.workspaceAssistant.text += event.assistantMessageEvent.delta || '';
        renderMarkdown(state.workspaceAssistant.node, state.workspaceAssistant.text);
      }
      if (event.type === 'message_end' && event.message?.role === 'assistant') {
        const text = Array.isArray(event.message.content) ? event.message.content.filter(part => part.type === 'text').map(part => part.text || '').join('\n') : typeof event.message.content === 'string' ? event.message.content : '';
        if (text) { const message = state.workspaceAssistant?.node || element('div', 'message-content pi-assistant-message'); renderMarkdown(message, text); if (!message.isConnected) $('workspace-messages').append(message); }
        state.workspaceAssistant = null;
      }
    }
    state.workspaceEvents = state.workspaceEvents.slice(-300);
    state.workspaceSequence = payload.next_seq ?? state.workspaceEvents.at(-1)?.seq ?? state.workspaceSequence;
    $('workspace-events').textContent = JSON.stringify(state.workspaceEvents, null, 2);
    renderWorkspace();
    if (['STARTING', 'READY', 'RUNNING', 'ABORTING'].includes(workspaceState()) && state.authenticated) state.workspaceTimer = setTimeout(pollWorkspace, 1500);
  } catch (error) { $('workspace-status').textContent = `Workspace polling stopped: ${error.message} Refresh to resume; no prompt is replayed.`; }
}

async function workspaceFiles(path = '') {
  if (!state.workspace?.id) return;
  const base = `/v1/workspaces/sessions/${encodeURIComponent(state.workspace.id)}`;
  try {
    const listing = await request(`${base}/files?path=${encodeURIComponent(path)}`);
    $('workspace-path').value = listing.path || '';
    $('workspace-files').replaceChildren(...(listing.entries || []).map(entry => {
      const button = element('button', 'file-entry', `${entry.type === 'directory' || entry.type === 'dir' ? '▸' : '·'} ${entry.name} ${entry.size === undefined ? '' : bytes(entry.size)}`); button.type = 'button';
      button.addEventListener('click', async () => {
        if (['directory', 'dir'].includes(entry.type)) return workspaceFiles(entry.path);
        try { const file = await request(`${base}/file?path=${encodeURIComponent(entry.path)}`); $('workspace-file-content').textContent = `${file.path}${file.truncated ? ' · truncated' : ''}\n\n${file.content || ''}`; }
        catch (error) { $('workspace-file-content').textContent = error.message; }
      }); return button;
    }));
  } catch (error) { $('workspace-file-content').textContent = error.message; }
}

function evidenceTable(entries) {
  if (!entries.length) return element('p', 'muted', "No recorded results.");
  const wrapper = element('div', 'evidence-table-wrap');
  const table = element('table', 'evidence-table');
  const head = element('tr');
  for (const title of ["Test / model", "Status", 'Decode TPS', 'HTTP TPS', "Context", "Evidence"]) head.append(element('th', '', title));
  const thead = element('thead'); thead.append(head); table.append(thead);
  const tbody = element('tbody');
  for (const entry of entries) {
    const row = element('tr');
    for (const value of [entry.label, entry.status, number(entry.decode_tps, 2), number(entry.http_tps, 2), number(entry.context_tokens, 0), [entry.report, entry.notes].filter(Boolean).join(' · ')]) row.append(element('td', '', value || '—'));
    tbody.append(row);
  }
  table.append(tbody); wrapper.append(table); return wrapper;
}

function renderCatalog() {
  const models = state.catalog?.models || [];
  $('models-list').replaceChildren(...models.map(model => {
    const card = element('article', 'catalog-card surface');
    const heading = element('div', 'catalog-heading');
    const label = element('div'); label.append(element('h2', '', model.name || model.id), element('p', 'muted small', model.id));
    const badge = element('span'); setBadge(badge, model.status); heading.append(label, badge); card.append(heading);
    card.append(element('p', 'catalog-detail', [model.format, model.runtime, model.architecture, model.distribution].filter(Boolean).join(' · ')));
    for (const [label, field] of [["Testability", 'testability'], ["N-gram embedding", 'ngram_embedding'], ['Draft / lookup', 'draft_lookup']]) {
      const row = element('p', 'catalog-detail');
      row.append(element('strong', '', `${label}: `), document.createTextNode(model[field] || "Not documented in the catalog."));
      card.append(row);
    }
    const detailPanel = element('details', 'catalog-evidence');
    detailPanel.append(element('summary', '', `Assets (${model.assets?.length || 0}), pinned sources and quality limits`));
    for (const note of [...(model.quality_limits || []), ...(model.blockers || [])]) detailPanel.append(element('p', 'small muted', note));
    const assets = element('div', 'catalog-assets');
    for (const asset of model.assets || []) {
      const row = element('div', 'asset-row');
      row.append(element('strong', '', `${asset.role || asset.id} · ${asset.node || "node not specified"}`), element('span', '', `${asset.present === true ? "Local" : asset.present === false ? "Not local" : "Presence not checked"} · ${bytes(asset.actual_bytes)} / ${bytes(asset.expected_bytes)} · ${asset.verification || "integrity not verified"}`), element('code', '', asset.path || ''));
      assets.append(row);
    }
    detailPanel.append(assets);
    for (const source of model.sources || []) {
      const url = safeSourceURL(source.url);
      const sourceNode = element(url ? 'a' : 'span', 'small source-link', [source.url, source.revision].filter(Boolean).join(' · '));
      if (url) { sourceNode.href = url; sourceNode.target = '_blank'; sourceNode.rel = 'noopener noreferrer'; }
      detailPanel.append(sourceNode);
    }
    card.append(detailPanel);
    const actions = element('div', 'inline-actions');
    for (const [name, action] of Object.entries(model.actions || {})) {
      const ids = name === 'download' ? (model.assets || []).map(asset => `download:${asset.id}`)
        : name === 'benchmark' ? (model.serving_model_id && model.serving_model_id === state.model ? ['speed-chat', 'speed-historical', 'api-smoke'] : []) : [`${name}:${model.id}`];
      const available = state.actions.filter(item => ids.includes(item.id) && item.available);
      const button = element('button', 'button secondary', name === 'load' ? "Load" : name === 'download' ? 'Download' : 'Benchmark');
      button.type = 'button';
      button.disabled = !action.enabled || !available.length;
      button.title = button.disabled ? action.reason || "Action unavailable in this controller build." : available[0].description;
      button.addEventListener('click', () => { selectTab('benchmarks'); });
      actions.append(button);
    }
    card.append(actions);
    if (model.evidence?.length) { const details = element('details', 'catalog-evidence'); details.append(element('summary', '', "Historical results (not new benchmarks)"), evidenceTable(model.evidence)); card.append(details); }
    return card;
  }));
  if (!models.length) $('models-list').textContent = "Empty catalog: models are not inferred from the filesystem.";
  $('benchmark-evidence').replaceChildren(evidenceTable(models.flatMap(model => (model.evidence || []).map(entry => ({ ...entry, label: `${model.name || model.id} · ${entry.label}` })))));
}

async function refreshCatalog() {
  try {
    state.catalog = await request('/v1/catalog');
    const options = await request('/v1/operations/options'); state.actions = options?.actions || [];
    renderCatalog();
  } catch (error) { $('models-list').replaceChildren(element('p', 'notice bad', `Catalog unavailable: ${error.message}`)); }
}

function renderOperations() {
  $('operation-actions').replaceChildren(...state.actions.map(action => {
    const row = element('div', 'operation-row');
    const detail = element('div'); detail.append(element('strong', '', action.label || action.id), element('p', 'muted small', [action.description, action.download_bytes ? bytes(action.download_bytes) : '', action.blocked_reason].filter(Boolean).join(' · ')));
    const button = element('button', 'button secondary', "Run…"); button.type = 'button'; button.disabled = state.operationSubmitting || !action.available || state.jobs.some(job => !isTerminal(job.status));
    button.addEventListener('click', () => startOperation(action)); row.append(detail, button); return row;
  }));
  $('operation-jobs').replaceChildren(...state.jobs.map(job => {
    const card = element('article', 'job surface');
    const heading = element('div', 'job-head');
    const badge = element('span'); setBadge(badge, job.status);
    heading.append(element('strong', '', `${job.action_id} · ${job.id}`), badge); card.append(heading);
    const progress = job.progress || {};
    card.append(element('p', 'small', [progress.phase, progress.total ? `${progress.current ?? 0} / ${progress.total}` : '', progress.total_bytes ? `${bytes(progress.bytes)} / ${bytes(progress.total_bytes)}` : '', job.error].filter(Boolean).join(' · ')));
    if (job.raw_directory) card.append(element('code', 'small', job.raw_directory));
    const logs = element('details'); logs.append(element('summary', '', "Result / log"), element('pre', '', JSON.stringify({ result: job.result, log_tail: job.log_tail }, null, 2))); card.append(logs);
    if (!isTerminal(job.status)) {
      const cancel = element('button', 'button secondary', "Cancel"); cancel.type = 'button'; cancel.disabled = !canCancelTask(job.status);
      cancel.addEventListener('click', async () => {
        if (!window.confirm("Request cancellation? The engine may need to drain; this is not GPU preemption.")) return;
        cancel.disabled = true;
        try { await request(`/v1/operations/jobs/${encodeURIComponent(job.id)}/cancel`, { method: 'POST', body: { confirm: true } }); await refreshBenchmarks(); }
        catch (error) { notice(`${error.message} No automatic retry.`, 'bad'); }
      });
      card.append(cancel);
    }
    return card;
  }));
  if (!state.jobs.length) $('operation-jobs').textContent = "No recorded jobs.";
}

async function refreshBenchmarks() {
  clearTimeout(state.jobTimer);
  try {
    const [options, jobs, catalog] = await Promise.all([request('/v1/operations/options'), request('/v1/operations/jobs'), request('/v1/catalog')]);
    state.actions = options?.actions || []; state.jobs = jobs?.jobs || []; state.catalog = catalog;
    renderOperations(); renderCatalog();
    if (state.jobs.some(job => !isTerminal(job.status)) && state.authenticated) state.jobTimer = setTimeout(refreshBenchmarks, 3000);
  } catch (error) { $('operation-jobs').textContent = `Status unavailable: ${error.message} Refresh to resume; no actions were resubmitted.`; }
}

async function startOperation(action) {
  if (state.operationSubmitting) return;
  if (!window.confirm(`${action.label || action.id}\n${action.description || ''}\n${action.download_bytes ? `Download: ${bytes(action.download_bytes)}. ` : ''}${action.requests ? `Model requests: ${action.requests}. ` : ''}\nRun this operation on the pair? Long contexts are not automatically qualified.`)) return;
  state.operationSubmitting = true;
  renderOperations();
  try {
    await request('/v1/operations/jobs', { method: 'POST', body: { action_id: action.id, confirm: true }, timeout: 30000 });
    await refreshBenchmarks();
  } catch (error) { notice(`${error.message} No automatic retry: inspect jobs before resubmitting.`, 'bad'); }
  finally { state.operationSubmitting = false; renderOperations(); }
}

for (const id of ['ui-language', 'ui-text-size', 'ui-density', 'show-thinking', 'ui-sidebar-collapsed', 'ui-show-advanced']) $(id).addEventListener('change', savePreferences);
$('refresh-settings').addEventListener('click', refreshSettings);
$('api-settings-form').addEventListener('submit', saveAPISettings);
$('rotate-api-token').addEventListener('click', rotateAPIToken);
$('copy-api-token').addEventListener('click', async () => {
  if (!state.token) { $('connection-result').textContent = state.language === 'it' ? 'Connettiti prima di copiare il token attivo.' : 'Connect before copying the active token.'; return; }
  try { await navigator.clipboard.writeText(state.token); $('connection-result').textContent = state.language === 'it' ? 'Token copiato negli appunti. Trattalo come una password.' : 'Token copied to clipboard. Treat it as a password.'; }
  catch { $('connection-result').textContent = state.language === 'it' ? 'Appunti non disponibili. Leggi il token dal file privato del server.' : 'Clipboard unavailable. Read the token from the private server file.'; }
});
$('chat-attachments').addEventListener('change', uploadAttachments);
$('export-chat').addEventListener('click', () => {
  downloadJSON({ schema: 1, exported_utc: new Date().toISOString(), model: state.model, conversation_id: state.conversationID, messages: state.records, replay_history: state.chat, note: 'Messages marked incomplete/error were not included in model replay history. Attachment IDs require the original server; extracted file text is not duplicated here.' }, `haloclu-conversation-${state.conversationID || 'new'}.json`);
});
$('workspace-kind').addEventListener('change', () => { $('workspace-ssh').hidden = $('workspace-kind').value !== 'ssh'; });
$('workspace-preset').addEventListener('change', () => { const root = $('workspace-preset').selectedOptions[0]?.dataset.root; if (root) $('workspace-root').value = root; });
$('workspace-refresh').addEventListener('click', async () => { await refreshWorkspaces(); if (state.workspace) await pollWorkspace(); });
$('workspace-session').addEventListener('change', async () => {
  clearTimeout(state.workspaceTimer); state.workspace = state.workspaceSessions.find(session => session.id === $('workspace-session').value) || null;
  state.workspaceSequence = 0; state.workspaceEvents = []; state.workspaceAssistant = null; $('workspace-messages').replaceChildren();
  renderWorkspace(); if (state.workspace) await pollWorkspace();
});
$('workspace-form').addEventListener('submit', async event => {
  event.preventDefault(); if (!state.authenticated || state.workspaceBusy) return;
  if (!window.confirm(`Create ${$('workspace-kind').value} workspace at ${$('workspace-root').value}? This does not send a model prompt.`)) return;
  state.workspaceBusy = true; renderWorkspace();
  const body = { kind: $('workspace-kind').value, root: $('workspace-root').value.trim(), reasoning_effort: $('reasoning-mode').value, confirm: true };
  if (body.kind === 'ssh') body.preset_id = $('workspace-preset').value;
  try { state.workspace = await request('/v1/workspaces/sessions', { method: 'POST', body, timeout: 60000 }); state.workspaceSequence = 0; state.workspaceEvents = []; $('workspace-messages').replaceChildren(); await refreshWorkspaces(); }
  catch (error) { notice(`${error.message} No automatic retry.`, 'bad'); }
  finally { state.workspaceBusy = false; renderWorkspace(); }
});
$('workspace-preset-form').addEventListener('submit', async event => {
  event.preventDefault(); if (!window.confirm('Save this SSH connection preset without a password?')) return;
  try { await request('/v1/workspaces/presets', { method: 'POST', body: { name: $('preset-name').value.trim(), host: $('preset-host').value.trim(), port: Number($('preset-port').value), user: $('preset-user').value.trim(), key_path: $('preset-key').value.trim(), root: $('preset-root').value.trim(), confirm: true } }); await refreshWorkspaces(); }
  catch (error) { notice(error.message, 'bad'); }
});
$('workspace-connect').addEventListener('click', () => workspaceAction('connect', $('workspace-password').value ? { password: $('workspace-password').value } : {}));
$('workspace-start').addEventListener('click', () => workspaceAction('start'));
$('workspace-abort').addEventListener('click', () => workspaceAction('abort'));
$('workspace-close').addEventListener('click', () => workspaceAction('close'));
$('workspace-prompt-form').addEventListener('submit', event => { event.preventDefault(); if (!$('workspace-send').disabled) workspaceAction('prompt', { message: $('workspace-prompt').value.trim() }); });
$('workspace-files-form').addEventListener('submit', event => { event.preventDefault(); workspaceFiles($('workspace-path').value.trim()); });
$('workspace-terminal-form').addEventListener('submit', async event => {
  event.preventDefault(); if (!state.workspace?.id || !window.confirm(`Run diagnostic ${$('workspace-command').value} in this workspace?`)) return;
  $('workspace-terminal-run').disabled = true;
  try { const output = await request(`/v1/workspaces/sessions/${encodeURIComponent(state.workspace.id)}/terminal`, { method: 'POST', body: { command_id: $('workspace-command').value, confirm: true }, timeout: 60000 }); $('workspace-terminal-output').textContent = typeof output.output === 'string' ? `${output.output}\n\nExit: ${output.exitcode ?? output.exit_code ?? 'not reported'} · ${output.seconds ?? output.time ?? 'not reported'} s` : JSON.stringify(output, null, 2); }
  catch (error) { $('workspace-terminal-output').textContent = error.message; }
  finally { renderWorkspace(); }
});
$('workspace-shell-form').addEventListener('submit', async event => {
  event.preventDefault();
  const command = $('workspace-shell-command').value.trim();
  if (!canRunWorkspaceShell(state.workspace) || state.workspaceBusy || !command) return;
  if (command.length > 4096) { notice(state.language === 'it' ? 'Il comando supera 4096 caratteri.' : 'Command exceeds 4096 characters.', 'bad'); return; }
  const root = state.workspace.root || state.workspace.id;
  const warning = state.workspace.kind === 'ssh'
    ? (state.language === 'it' ? 'SSH: usa i privilegi dell’account remoto e può modificare o eliminare file.' : 'SSH: this runs with the remote account privileges and can modify or delete files.')
    : (state.language === 'it' ? 'Locale: agisce nella sandbox workspace e può modificare o eliminare file subito. Non esiste un Applica differito.' : 'Local: this runs inside the workspace sandbox and can modify or delete workspace files immediately. There is no deferred Apply step.');
  if (!window.confirm(`${warning}\nWorkspace: ${root}\n\n${command}\n\n${state.language === 'it' ? 'Eseguire esattamente questo comando?' : 'Run this exact command?'}`)) return;
  state.workspaceBusy = true; renderWorkspace();
  try {
    const output = await request(`/v1/workspaces/sessions/${encodeURIComponent(state.workspace.id)}/terminal`, { method: 'POST', body: { command, confirm: true }, timeout: 65000 });
    $('workspace-terminal-output').textContent = `${output.output || ''}\n\nExit: ${output.exit_code ?? output.exitcode ?? 'not reported'} · ${output.seconds ?? 'not reported'} s${output.truncated ? '\nOUTPUT TRUNCATED' : ''}`;
    await pollWorkspace();
  } catch (error) { $('workspace-terminal-output').textContent = `${error.message}\nNo automatic retry. Completion may be uncertain; refresh session state and inspect events before resubmitting.`; }
  finally { state.workspaceBusy = false; renderWorkspace(); }
});

document.querySelectorAll('[data-tab]').forEach(tab => {
  tab.addEventListener('click', () => selectTab(tab.dataset.tab));
  tab.addEventListener('keydown', event => {
    if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End'].includes(event.key)) return;
    event.preventDefault();
    const tabs = [...document.querySelectorAll('[data-tab]')].filter(node => !node.hidden);
    const index = tabs.indexOf(tab);
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1
      : (index + (['ArrowLeft', 'ArrowUp'].includes(event.key) ? -1 : 1) + tabs.length) % tabs.length;
    selectTab(tabs[next].dataset.tab, true);
  });
});
document.querySelectorAll('[data-go-coding]').forEach(button => button.addEventListener('click', () => selectTab('workspace', true)));
$('reasoning-mode').addEventListener('change', () => { state.reasoning = $('reasoning-mode').value; });
for (const id of ['sidebar-toggle', 'sidebar-collapse']) $(id).addEventListener('click', () => { $('ui-sidebar-collapsed').checked = !document.body.classList.contains('sidebar-collapsed'); savePreferences(); });
$('refresh-models').addEventListener('click', refreshCatalog);
$('refresh-benchmarks').addEventListener('click', refreshBenchmarks);
$('connection-toggle').addEventListener('click', () => showConnection());
$('open-connection').addEventListener('click', () => showConnection());
$('connection-form').addEventListener('submit', async event => {
  event.preventDefault();
  state.token = $('api-token').value.trim();
  await refreshHealth(true);
  if (state.authenticated) showConnection(false);
});
$('forget-token').addEventListener('click', () => {
  state.token = '';
  $('api-token').value = '';
  state.authenticated = false;
  state.settings = null;
  $('api-settings-fields').disabled = true;
  $('rotate-api-token').disabled = true;
  clearTimeout(state.taskTimer);
  clearTimeout(state.jobTimer);
  clearTimeout(state.workspaceTimer);
  $('connection-label').textContent = "Token forgotten";
  $('connection-dot').className = 'status-dot';
  $('connection-result').textContent = "Token removed from this tab. Existing server tasks are not cancelled.";
});
$('refresh-health').addEventListener('click', () => refreshHealth(true));
$('refresh-cluster').addEventListener('click', () => refreshHealth(true));
$('chat-form').addEventListener('submit', sendChat);
$('chat-input').addEventListener('keydown', event => {
  if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) { event.preventDefault(); $('chat-form').requestSubmit(); }
});
$('stop-chat').addEventListener('click', () => state.chatController?.abort());
$('clear-chat').addEventListener('click', () => {
  if (state.chatController) return;
  state.chat = [];
  state.records = [];
  state.conversationID = Math.max(0, ...state.conversations.map(item => item.id)) + 1;
  document.querySelectorAll('.message').forEach(node => node.remove());
  $('chat-empty').hidden = false;
  for (const id of ['chat-live-tps', 'chat-tps', 'chat-ttft', 'chat-wall', 'chat-tokens', 'chat-context']) $(id).textContent = '—';
  renderConversations();
  $('chat-input').focus();
});
$('coding-form').addEventListener('submit', startTask);
$('code-spec').addEventListener('change', async () => {
  const file = $('code-spec').files[0];
  if (!file) return;
  try {
    if (file.size > 128 * 1024) throw new Error('Task specification exceeds 128 KiB. Use source paths, not embedded repositories.');
    const spec = importedTaskSpec(JSON.parse(await file.text()));
    if (spec.thinking_token_budget !== undefined && state.options?.thinking_budget_supported !== true) throw new Error("The server does not support the imported thinking budget. No parameters were applied.");
    $('code-task').value = spec.task;
    $('code-repo').value = spec.repo;
    $('code-paths').value = spec.allowed_paths.join('\n');
    $('code-build').value = spec.build_command;
    $('code-test').value = spec.test_command;
    $('reasoning-mode').value = spec.reasoning_effort;
    state.reasoning = spec.reasoning_effort;
    $('code-timeout').value = Math.min(spec.timeout, Number($('code-timeout').max));
    $('code-repairs').value = spec.max_repairs;
    state.importedOptions = {};
    for (const field of ['files', 'test_files']) if (spec[field] !== undefined) state.importedOptions[field] = spec[field];
    if (spec.context_tokens && state.options?.context_options.includes(spec.context_tokens)) $('context-select').value = spec.context_tokens;
    if (spec.max_tokens) {
      if (![...$('chat-cap').options].some(option => Number(option.value) === spec.max_tokens)) { const option = element('option', '', `${number(spec.max_tokens, 0)} (import)`); option.value = spec.max_tokens; $('chat-cap').append(option); }
      $('chat-cap').value = spec.max_tokens;
    } else $('chat-cap').value = '';
    if (spec.thinking_token_budget !== undefined) {
      const value = String(spec.thinking_token_budget);
      if (![...$('thinking-budget').options].some(option => option.value === value)) { const option = element('option', '', `${value} token (import)`); option.value = value; $('thinking-budget').append(option); }
      $('thinking-budget').value = value;
    } else $('thinking-budget').value = '';
    const details = [`Imported ${file.name}`];
    if (spec.legacy_profile) details.push(`legacy profile ${spec.legacy_profile}: low alias, effective reasoning ${spec.reasoning_effort}`);
    if (spec.thinking_token_budget !== undefined) details.push(`thinking budget ${spec.thinking_token_budget}`);
    if (spec.files) details.push(`${spec.files.length} context files`);
    if (spec.test_files) details.push(`${Object.keys(spec.test_files).length} isolated test fixtures (not model context)`);
    if (spec.context_tokens) details.push(`requested window ${spec.context_tokens}; review the active sidebar selection`);
    if (spec.max_tokens) details.push(`output cap ${spec.max_tokens}`);
    $('import-summary').textContent = details.join(' · ') + '. Review the form before running. Source paths are still validated by the server.';
    $('clear-import').hidden = false;
    $('coding-error').hidden = true;
  } catch (error) {
    $('coding-error').textContent = `Cannot import task JSON: ${error.message}`;
    $('coding-error').hidden = false;
  } finally {
    $('code-spec').value = '';
  }
});
$('clear-import').addEventListener('click', () => {
  state.importedOptions = {};
  $('import-summary').textContent = 'Extra imported context/test options cleared. Visible form fields remain unchanged.';
  $('clear-import').hidden = true;
});
$('resume-form').addEventListener('submit', event => {
  event.preventDefault();
  const id = $('resume-id').value.trim();
  if (!id) return;
  if (!/^[a-zA-Z0-9._-]{1,160}$/.test(id)) { $('task-action-result').textContent = 'Invalid task ID.'; return; }
  state.taskGeneration++;
  clearTimeout(state.taskTimer);
  state.taskID = id;
  pollTask(id, state.taskGeneration);
});
$('refresh-task').addEventListener('click', () => { clearTimeout(state.taskTimer); pollTask(); });
$('cancel-task').addEventListener('click', () => taskAction('cancel'));
$('apply-task').addEventListener('click', () => taskAction('apply'));
$('copy-diff').addEventListener('click', async () => {
  try { await navigator.clipboard.writeText(state.task?.diff || ''); $('task-action-result').textContent = 'Diff copied.'; }
  catch { $('task-action-result').textContent = 'Clipboard unavailable. Use Save or select the diff manually.'; }
});
$('download-diff').addEventListener('click', () => {
  const url = URL.createObjectURL(new Blob([state.task?.diff || ''], { type: 'text/x-diff;charset=utf-8' }));
  const anchor = element('a');
  anchor.href = url;
  anchor.download = `strixglm-${state.taskID.replace(/[^a-zA-Z0-9._-]/g, '_')}.patch`;
  anchor.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
});
document.addEventListener('visibilitychange', () => {
  if (!document.hidden && state.authenticated) refreshHealth();
});
state.healthTimer = setInterval(() => {
  if (!document.hidden && state.authenticated && !state.chatController) refreshHealth();
}, 15000);
initializeLanguage();
refreshHealth();
