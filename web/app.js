import {
  SSEParser, activeRequestLabel, bytes, canCancelTask, classifyStatus, completionDelta, completionState, decodeRate,
  errorMessage, finite, healthStatus, importedTaskSpec, isSuccess, isTerminal, normalizeTask, number,
  pathList, percent, seconds,
} from './ui-core.mjs';

const $ = id => document.getElementById(id);
const state = {
  token: '', model: '', profile: 'fast', profiles: {}, profileStatus: '', contextLimit: null,
  chat: [], chatController: null, task: null, taskID: '', taskTimer: null,
  taskGeneration: 0, healthBusy: false, authenticated: false, healthTimer: null,
  importedOptions: {},
};

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
  $('connection-panel').hidden = !show;
  $('connection-toggle').setAttribute('aria-expanded', String(show));
  if (show) $('api-token').focus();
}

function setBadge(node, status) {
  node.textContent = String(status || 'unknown').toUpperCase();
  node.className = `badge ${classifyStatus(status)}`;
}

function selectTab(name, focus = false) {
  for (const tab of document.querySelectorAll('[data-tab]')) {
    const active = tab.dataset.tab === name;
    tab.classList.toggle('active', active);
    tab.setAttribute('aria-selected', String(active));
    tab.tabIndex = active ? 0 : -1;
    if (active && focus) tab.focus();
    $(`panel-${tab.dataset.tab}`).hidden = !active;
  }
  $('view-label').textContent = name.toUpperCase();
}

function headers() {
  const result = { Accept: 'application/json' };
  if (state.token) result.Authorization = `Bearer ${state.token}`;
  return result;
}

async function request(path, options = {}) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), options.timeout || 15000);
  try {
    const response = await fetch(path, {
      method: options.method || 'GET', credentials: 'omit', cache: 'no-store',
      mode: 'same-origin', redirect: 'error', signal: controller.signal,
      headers: { ...headers(), ...(options.body ? { 'Content-Type': 'application/json' } : {}) },
      body: options.body ? JSON.stringify(options.body) : undefined,
    });
    const payload = await response.json().catch(() => null);
    if (!response.ok) {
      if (response.status === 401 || response.status === 403) {
        state.authenticated = false;
        $('connection-label').textContent = 'Token required';
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

function setProfile(profile) {
  state.profile = profile;
  $('code-profile').value = profile;
  for (const button of document.querySelectorAll('[data-profile]')) {
    const active = button.dataset.profile === profile;
    button.classList.toggle('active', active);
    button.setAttribute('aria-pressed', String(active));
  }
  const definition = state.profiles[profile];
  if (definition && typeof definition === 'object') {
    const reasoning = definition.reasoning || definition.reasoning_effort;
    const context = definition.context_tokens || definition.context;
    const pieces = [`${profile.toUpperCase()} server preset`];
    if (reasoning) pieces.push(`reasoning ${reasoning}`);
    if (context) pieces.push(`estimated context-selection budget ${number(context, 0)} tokens; not a qualified actual-token limit`);
    if (definition.description) pieces.push(definition.description);
    if (state.profileStatus) pieces.push(`qualification: ${state.profileStatus}`);
    $('profile-note').textContent = pieces.join(' · ');
  } else {
    $('profile-note').textContent = 'Profile parameters come from the server configuration; “Quality” does not automatically mean maximum reasoning.';
  }
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
  if (status?.profiles && typeof status.profiles === 'object') {
    state.profiles = status.profiles;
    state.profileStatus = typeof status.profile_status === 'string' ? status.profile_status : '';
    setProfile(state.profile);
  }
}

async function refreshHealth(interactive = false) {
  if (state.healthBusy) return;
  state.healthBusy = true;
  $('refresh-health').disabled = true;
  $('refresh-cluster').disabled = true;
  try {
    const health = await request('/health');
    const [status, models] = await Promise.all([request('/v1/status'), request('/v1/models')]);
    state.authenticated = true;
    state.model = status?.model || models?.data?.[0]?.id || '';
    renderHealth(health, status);
    const healthState = healthStatus(status?.health, healthStatus(health));
    const bad = classifyStatus(healthState) === 'bad';
    $('connection-label').textContent = bad ? 'Engine needs attention' : 'Local API connected';
    $('connection-dot').className = `status-dot ${bad ? 'bad' : 'good'}`;
    $('connection-result').textContent = bad ? 'API reachable; inspect cluster state before generating.' : 'Authenticated. This tab can send chat and isolated coding tasks.';
    if (interactive) notice(bad ? 'The API is reachable, but the engine reports an unhealthy state.' : '', bad ? 'bad' : 'neutral');
  } catch (error) {
    state.authenticated = false;
    $('connection-dot').className = 'status-dot bad';
    if (!$('connection-label').textContent.includes('Token')) $('connection-label').textContent = 'Connection unavailable';
    $('model-pill').textContent = 'GLM · API unavailable';
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
  const avatar = element('span', 'message-avatar', role === 'assistant' ? 'GLM' : 'YOU');
  const body = element('div');
  const meta = element('div', 'message-meta', role === 'assistant' ? `${state.profile.toUpperCase()} · GLM` : 'YOU');
  const text = element('pre', 'message-content', content);
  const details = element('details', 'reasoning-details');
  const summary = element('summary', '', 'Reasoning');
  const reasoning = element('pre');
  details.append(summary, reasoning);
  details.hidden = true;
  const error = element('p', 'message-error');
  error.hidden = true;
  body.append(meta, details, text, error);
  outer.append(avatar, body);
  $('conversation').append(outer);
  return { outer, meta, text, details, reasoning, error };
}

function scrollChat() {
  const container = $('conversation');
  if (container.scrollHeight - container.scrollTop - container.clientHeight < 300) container.scrollTop = container.scrollHeight;
}

function updateChatMetrics(usage, timings, firstTokenMS, started, finished = false) {
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
  const prompt = finite(usage?.prompt_tokens);
  $('chat-context').textContent = prompt === null ? '—'
    : `${number(prompt, 0)}${state.contextLimit ? ` / ${number(state.contextLimit, 0)}` : ''}`;
  $('chat-context').title = 'Server-reported prompt tokens. This is not a local token estimate.';
}

async function sendChat(event) {
  event.preventDefault();
  if (state.chatController) return;
  const input = $('chat-input').value.trim();
  if (!input) return;
  if (!state.authenticated || !state.model) { showConnection(); notice('Connect the local API before sending a message.'); return; }
  const cap = Number($('chat-cap').value);
  if (!Number.isSafeInteger(cap) || cap < 32 || cap > 16384) { notice('Output cap must be an integer between 32 and 16384.', 'bad'); return; }
  notice('');
  const requestProfile = state.profile;
  const userMessage = { role: 'user', content: input };
  message('user', input);
  const output = message('assistant');
  output.meta.textContent += ' · connecting';
  $('chat-input').value = '';
  const controller = new AbortController();
  state.chatController = controller;
  $('send-chat').disabled = true;
  $('clear-chat').disabled = true;
  $('stop-chat').hidden = false;
  const started = performance.now();
  let text = '', reasoning = '', finish = null, usage = null, timings = null;
  let firstTokenMS = null, done = false, protocolError = null;
  const ticker = setInterval(() => updateChatMetrics(usage, timings, firstTokenMS, started), 500);
  function receive(payload) {
    if (payload?.error) { protocolError = errorMessage(payload); return; }
    const delta = completionDelta(payload);
    if ((delta.content || delta.reasoning) && firstTokenMS === null) firstTokenMS = performance.now() - started;
    text += delta.content;
    reasoning += delta.reasoning;
    if (delta.finish) finish = delta.finish;
    if (delta.usage) usage = delta.usage;
    if (delta.timings) timings = delta.timings;
    output.text.textContent = text;
    output.reasoning.textContent = reasoning;
    output.details.hidden = !reasoning;
    output.meta.textContent = `${requestProfile.toUpperCase()} · ${finish || 'streaming'}`;
    scrollChat();
  }
  try {
    const response = await fetch('/v1/chat/completions', {
      method: 'POST', credentials: 'omit', cache: 'no-store', mode: 'same-origin', redirect: 'error',
      signal: controller.signal,
      headers: { ...headers(), Accept: 'text/event-stream', 'Content-Type': 'application/json' },
      body: JSON.stringify({ model: state.model, profile: requestProfile, messages: [...state.chat, userMessage], stream: true, stream_options: { include_usage: true }, max_tokens: cap }),
    });
    if (!response.ok) {
      const failure = await response.json().catch(() => null);
      throw new Error(errorMessage(failure, `HTTP ${response.status}`));
    }
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
    output.meta.textContent = `${requestProfile.toUpperCase()} · ${resultState === 'complete' ? 'complete' : 'incomplete'}`;
  } catch (error) {
    output.error.hidden = false;
    output.error.textContent = error.name === 'AbortError'
      ? 'Stream stopped. Partial output was not added to history; the engine may be draining the request.' : error.message;
    output.meta.textContent = `${requestProfile.toUpperCase()} · ${error.name === 'AbortError' ? 'stopped' : 'error'}`;
  } finally {
    clearInterval(ticker);
    state.chatController = null;
    updateChatMetrics(usage, timings, firstTokenMS, started, true);
    $('send-chat').disabled = false;
    $('clear-chat').disabled = false;
    $('stop-chat').hidden = true;
    $('chat-input').focus();
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
  const heading = element('span', '', `Attempt ${attempt.index ?? index + 1} · ${attempt.reasoning || attempt.profile || 'profile not reported'}`);
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
  $('task-tps').textContent = number(metrics.decode_tps, 2);
  $('task-tps').title = 'Measured decode speed of the last model attempt, not aggregate task throughput. Wall time includes all attempts.';
  $('task-profile').textContent = task.profile || '—';
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
  const spec = {
    ...state.importedOptions,
    task: $('code-task').value.trim(), repo: $('code-repo').value.trim(),
    allowed_paths: pathList($('code-paths').value), test_command: $('code-test').value.trim(),
    build_command: $('code-build').value.trim(), timeout: Number($('code-timeout').value),
    profile: $('code-profile').value, max_repairs: Number($('code-repairs').value),
    sandbox_policy: 'isolated', apply: false,
  };
  if (!spec.task || !spec.repo.startsWith('/') || !spec.allowed_paths.length || !spec.test_command) {
    $('coding-error').textContent = 'Provide an instruction, absolute repository path, allowed source paths and a test command.';
    $('coding-error').hidden = false;
    return;
  }
  if (!Number.isSafeInteger(spec.timeout) || !Number.isSafeInteger(spec.max_repairs)) return;
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

document.querySelectorAll('[data-tab]').forEach((tab, index, tabs) => {
  tab.addEventListener('click', () => selectTab(tab.dataset.tab));
  tab.addEventListener('keydown', event => {
    if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End'].includes(event.key)) return;
    event.preventDefault();
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1
      : (index + (['ArrowLeft', 'ArrowUp'].includes(event.key) ? -1 : 1) + tabs.length) % tabs.length;
    selectTab(tabs[next].dataset.tab, true);
  });
});
document.querySelectorAll('[data-go-coding]').forEach(button => button.addEventListener('click', () => selectTab('coding', true)));
document.querySelectorAll('[data-profile]').forEach(button => button.addEventListener('click', () => setProfile(button.dataset.profile)));
$('connection-toggle').addEventListener('click', () => showConnection($('connection-panel').hidden));
$('open-connection').addEventListener('click', () => showConnection($('connection-panel').hidden));
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
  clearTimeout(state.taskTimer);
  $('connection-label').textContent = 'Token forgotten';
  $('connection-dot').className = 'status-dot';
  $('connection-result').textContent = 'Token removed from this tab. Existing server tasks are not cancelled.';
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
  document.querySelectorAll('.message').forEach(node => node.remove());
  $('chat-empty').hidden = false;
  for (const id of ['chat-tps', 'chat-ttft', 'chat-wall', 'chat-tokens', 'chat-context']) $(id).textContent = '—';
  $('chat-input').focus();
});
$('coding-form').addEventListener('submit', startTask);
$('code-spec').addEventListener('change', async () => {
  const file = $('code-spec').files[0];
  if (!file) return;
  try {
    if (file.size > 128 * 1024) throw new Error('Task specification exceeds 128 KiB. Use source paths, not embedded repositories.');
    const spec = importedTaskSpec(JSON.parse(await file.text()));
    $('code-task').value = spec.task;
    $('code-repo').value = spec.repo;
    $('code-paths').value = spec.allowed_paths.join('\n');
    $('code-build').value = spec.build_command;
    $('code-test').value = spec.test_command;
    $('code-profile').value = spec.profile;
    $('code-timeout').value = spec.timeout;
    $('code-repairs').value = spec.max_repairs;
    state.importedOptions = {};
    for (const field of ['files', 'test_files', 'context_tokens', 'max_tokens']) if (spec[field] !== undefined) state.importedOptions[field] = spec[field];
    const details = [`Imported ${file.name}`];
    if (spec.files) details.push(`${spec.files.length} context files`);
    if (spec.test_files) details.push(`${Object.keys(spec.test_files).length} isolated test fixtures (not model context)`);
    if (spec.context_tokens) details.push(`estimated context-selection budget ${spec.context_tokens}; not actual-token qualification`);
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
refreshHealth();
