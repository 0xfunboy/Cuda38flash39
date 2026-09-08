// Pure, dependency-free presentation helpers. Keep this module DOM-independent.
export const TERMINAL_STATES = new Set([
  'passed', 'pass', 'success', 'succeeded', 'completed', 'failed', 'fail', 'error',
  'cancelled', 'canceled', 'incomplete', 'timeout', 'applied', 'blocked', 'interrupted',
]);

export function isTerminal(status) {
  return TERMINAL_STATES.has(String(status || '').toLowerCase());
}

export function isSuccess(status) {
  return ['passed', 'pass', 'success', 'succeeded', 'applied'].includes(String(status || '').toLowerCase());
}

export function canCancelTask(status) {
  return ['queued', 'running'].includes(String(status || '').toLowerCase());
}

export function classifyStatus(status) {
  const value = String(status || '').toLowerCase();
  if (isSuccess(value) || ['ok', 'healthy', 'ready', 'online'].includes(value)) return 'good';
  if (['failed', 'fail', 'error', 'poisoned', 'unhealthy', 'offline', 'blocked'].includes(value)) return 'bad';
  return 'neutral';
}

export function healthStatus(value, fallback = 'unknown') {
  if (typeof value === 'string') return value;
  if (typeof value?.status === 'string') return value.status;
  if (value?.ok === true) return 'ok';
  if (value?.ok === false) return 'unhealthy';
  return fallback;
}

export function activeRequestLabel(value, busy) {
  if (Array.isArray(value)) return value.length ? value.join(' · ') : busy === true ? 'Active' : 'Idle';
  if (value === true || busy === true) return 'Active';
  if (value === false || value === null || busy === false) return 'Idle';
  if (typeof value === 'string') return value;
  if (value?.id) return String(value.id);
  return '—';
}

export function escapeHTML(value) {
  return String(value).replace(/[&<>"']/g, character => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  })[character]);
}

export function finite(value) {
  if (value === null || value === undefined || value === '' || typeof value === 'boolean') return null;
  const number = Number(value);
  return Number.isFinite(number) ? number : null;
}

export function number(value, digits = 1) {
  const result = finite(value);
  return result === null ? '—' : result.toLocaleString('en-GB', { maximumFractionDigits: digits });
}

export function seconds(value) {
  const result = finite(value);
  if (result === null) return '—';
  if (result < 1) return `${Math.round(result * 1000)} ms`;
  if (result < 60) return `${number(result, 2)} s`;
  return `${Math.floor(result / 60)}m ${Math.round(result % 60)}s`;
}

export function bytes(value) {
  const result = finite(value);
  if (result === null) return '—';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let scaled = result;
  let unit = 0;
  while (Math.abs(scaled) >= 1024 && unit < units.length - 1) { scaled /= 1024; unit++; }
  return `${number(scaled, 1)} ${units[unit]}`;
}

export function percent(value) {
  const result = finite(value);
  return result === null ? '—' : `${number(result * 100, 1)}%`;
}

export function pathList(value) {
  return [...new Set(String(value || '').split(/[\n,]/).map(path => path.trim()).filter(Boolean))];
}

export function commandText(value) {
  if (typeof value === 'string') return value;
  if (!Array.isArray(value) || value.some(part => typeof part !== 'string')) throw new Error('Commands must be a string or an array of strings.');
  return value.map(part => /^[a-zA-Z0-9_./:=+-]+$/.test(part) ? part : `'${part.replaceAll("'", "'\\''")}'`).join(' ');
}

export function importedTaskSpec(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Task JSON must be an object.');
  if (typeof value.task !== 'string' || !value.task.trim()) throw new Error('Task JSON requires a task instruction.');
  if (typeof value.repo !== 'string' || !value.repo.startsWith('/')) throw new Error('Task JSON requires an absolute repository path.');
  const stringList = (input, label) => {
    if (!Array.isArray(input) || input.some(part => typeof part !== 'string' || !part.trim())) throw new Error(`${label} must be an array of nonempty strings.`);
    return input;
  };
  const result = {
    task: value.task, repo: value.repo,
    allowed_paths: stringList(value.allowed_paths, 'allowed_paths'),
    test_command: commandText(value.test_command),
    build_command: value.build_command ? commandText(value.build_command) : '',
    profile: ['fast', 'balanced', 'quality'].includes(value.profile) ? value.profile : 'fast',
    timeout: value.timeout ?? 300, max_repairs: value.max_repairs ?? 2,
  };
  if (!result.allowed_paths.length || !result.test_command) throw new Error('Allowed source paths and a test command are required.');
  for (const [field, minimum, maximum] of [['timeout', 10, 1800], ['max_repairs', 0, 6], ['context_tokens', 256, 100000], ['max_tokens', 64, 16384]]) {
    const item = value[field] ?? result[field];
    if (item === undefined) continue;
    if (!Number.isSafeInteger(item) || item < minimum || item > maximum) throw new Error(`Invalid ${field} in task JSON.`);
    result[field] = item;
  }
  if (value.files !== undefined) result.files = stringList(value.files, 'files');
  if (value.test_files !== undefined) {
    if (!value.test_files || typeof value.test_files !== 'object' || Array.isArray(value.test_files)) throw new Error('test_files must map sandbox paths to absolute fixture paths.');
    for (const [name, path] of Object.entries(value.test_files)) {
      if (!name || typeof path !== 'string' || !path.startsWith('/')) throw new Error('Invalid isolated test fixture path.');
    }
    result.test_files = value.test_files;
  }
  // Deliberately do not import apply, auth, endpoint, sandbox policy, or tools.
  return result;
}

// SSE permits CR, LF, CRLF, multiple data lines, comments and arbitrary byte
// boundaries. The caller streams through TextDecoder before passing strings here.
export class SSEParser {
  constructor(onEvent, maxBufferedCharacters = 2 * 1024 * 1024) {
    this.onEvent = onEvent;
    this.limit = maxBufferedCharacters;
    this.line = '';
    this.data = [];
    this.event = '';
    this.id = '';
    this.pendingCR = false;
    this.size = 0;
    this.first = true;
  }

  push(chunk) {
    for (const character of chunk) {
      if (this.first) {
        this.first = false;
        if (character === '\uFEFF') continue;
      }
      if (this.pendingCR) {
        this.pendingCR = false;
        if (character === '\n') continue;
      }
      if (character === '\r' || character === '\n') {
        this.consumeLine();
        this.pendingCR = character === '\r';
      } else {
        this.line += character;
        if (++this.size > this.limit) throw new Error('SSE event exceeded the safe size limit.');
      }
    }
  }

  consumeLine() {
    const line = this.line;
    this.line = '';
    if (!line) {
      if (this.data.length) {
        this.onEvent({ event: this.event || 'message', data: this.data.join('\n'), id: this.id });
      }
      this.data = [];
      this.event = '';
      this.size = 0;
      return;
    }
    if (line.startsWith(':')) return;
    const separator = line.indexOf(':');
    const field = separator < 0 ? line : line.slice(0, separator);
    let value = separator < 0 ? '' : line.slice(separator + 1);
    if (value.startsWith(' ')) value = value.slice(1);
    if (field === 'data') this.data.push(value);
    if (field === 'event') this.event = value;
    if (field === 'id' && !value.includes('\0')) this.id = value;
  }

  finish() {
    // An unterminated event is discarded, as required by the SSE protocol.
    this.line = '';
    this.data = [];
    this.event = '';
    this.size = 0;
  }
}

export function completionDelta(payload) {
  const choice = payload?.choices?.[0];
  const delta = choice?.delta || choice?.message || {};
  return {
    content: typeof delta.content === 'string' ? delta.content : '',
    reasoning: typeof delta.reasoning_content === 'string' ? delta.reasoning_content
      : (typeof delta.reasoning === 'string' ? delta.reasoning : ''),
    finish: choice?.finish_reason || null,
    usage: payload?.usage || null,
    timings: payload?.timings || payload?.metrics || null,
  };
}

export function decodeRate(timings, usage) {
  const direct = finite(timings?.decode_tps ?? timings?.predicted_per_second);
  if (direct !== null) return direct;
  const generationMS = finite(timings?.generation_time_ms);
  const tokens = finite(usage?.completion_tokens);
  return generationMS > 0 && tokens > 1 ? (tokens - 1) * 1000 / generationMS : null;
}

export function completionState(done, finish, content) {
  if (done !== true) return 'incomplete-stream';
  if (finish === 'length') return 'incomplete-cap';
  if (finish !== 'stop' || typeof content !== 'string' || !content.trim()) return 'no-complete-final';
  return 'complete';
}

export function errorMessage(payload, fallback = 'Request failed.') {
  if (typeof payload === 'string' && payload) return payload.slice(0, 1600);
  const message = payload?.error?.message || payload?.message || payload?.error;
  return typeof message === 'string' ? message.slice(0, 1600) : fallback;
}

export function normalizeTask(payload) {
  const task = payload?.task && typeof payload.task === 'object' ? payload.task : payload || {};
  const result = task.result && typeof task.result === 'object' ? task.result : {};
  const merged = { ...task, ...result };
  return {
    ...merged,
    id: task.id || task.task_id || merged.id || '',
    status: task.status || merged.status || 'unknown',
    profile: merged.profile || merged.reasoning_profile || merged.profile_used || '',
    attempts: Array.isArray(merged.attempts) ? merged.attempts : [],
    files_changed: Array.isArray(merged.files_changed) ? merged.files_changed
      : (merged.files && typeof merged.files === 'object' ? Object.keys(merged.files) : []),
    metrics: merged.metrics || {},
    diff: typeof merged.diff === 'string' ? merged.diff : (typeof merged.patch === 'string' ? merged.patch : ''),
  };
}
