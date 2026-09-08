import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import {
  SSEParser, activeRequestLabel, bytes, canCancelTask, classifyStatus, commandText, completionDelta, completionState, decodeRate,
  errorMessage, escapeHTML, finite, healthStatus, importedTaskSpec, isSuccess, isTerminal, normalizeTask,
  number, pathList, percent, seconds,
} from '../ui-core.mjs';

function parseChunks(chunks) {
  const events = [];
  const parser = new SSEParser(event => events.push(event));
  for (const chunk of chunks) parser.push(chunk);
  parser.finish();
  return events;
}

test('SSE JSON and DONE survive every possible text chunk boundary', () => {
  const input = ': heartbeat\r\ndata: {"choices":[{"delta":{"content":"héllo 🌱"}}]}\r\n\r\ndata: [DONE]\n\n';
  const expected = parseChunks([input]);
  assert.equal(expected.length, 2);
  assert.equal(JSON.parse(expected[0].data).choices[0].delta.content, 'héllo 🌱');
  for (let split = 0; split <= input.length; split++) {
    assert.deepEqual(parseChunks([input.slice(0, split), input.slice(split)]), expected);
  }
  assert.deepEqual(parseChunks([...input]), expected);
});

test('SSE TextDecoder handles UTF-8 split inside multibyte characters', () => {
  const encoded = new TextEncoder().encode('data: {"text":"à界🛠️"}\n\n');
  for (let split = 0; split <= encoded.length; split++) {
    const decoder = new TextDecoder();
    const parsed = parseChunks([
      decoder.decode(encoded.slice(0, split), { stream: true }),
      decoder.decode(encoded.slice(split), { stream: true }), decoder.decode(),
    ]);
    assert.deepEqual(JSON.parse(parsed[0].data), { text: 'à界🛠️' });
  }
});

test('SSE supports BOM, multiline data, comments, persistent id and CR newlines', () => {
  const parsed = parseChunks(['\uFEFFid: 21\revent: update\rdata: first\rdata: second\r\r: ping\rdata:\r\r']);
  assert.deepEqual(parsed, [
    { event: 'update', data: 'first\nsecond', id: '21' },
    { event: 'message', data: '', id: '21' },
  ]);
});

test('SSE discards incomplete events and rejects oversized ones', () => {
  assert.deepEqual(parseChunks(['data: incomplete\n']), []);
  assert.deepEqual(parseChunks(['data: incomplete']), []);
  assert.throws(() => new SSEParser(() => {}, 8).push('data: 12345'), /size limit/);
  const events = [];
  const parser = new SSEParser(event => events.push(event), 20);
  for (let index = 0; index < 100; index++) parser.push('data: hi\n\n');
  assert.equal(events.length, 100);
});

test('SSE ignores NUL ids and does not strip extra whitespace', () => {
  const parsed = parseChunks(['id: retained\nid: ignored\0id\ndata:  two spaces before value\n\n']);
  assert.equal(parsed[0].id, 'retained');
  assert.equal(parsed[0].data, ' two spaces before value');
});

test('OpenAI delta accepts separate reasoning, usage-only packets and no choice', () => {
  assert.deepEqual(completionDelta({ choices: [{ delta: { content: 'code', reasoning_content: 'think' }, finish_reason: 'stop' }] }), {
    content: 'code', reasoning: 'think', finish: 'stop', usage: null, timings: null,
  });
  assert.equal(completionDelta({ choices: [], usage: { completion_tokens: 21 } }).usage.completion_tokens, 21);
  assert.equal(completionDelta({ choices: [{ delta: { content: { evil: true } } }] }).content, '');
  assert.equal(completionDelta({ choices: [{ message: { content: 'final' } }] }).content, 'final');
});

test('Unknown or non-finite metrics stay unavailable; zero is real data', () => {
  for (const value of [undefined, null, '', false, true, NaN, Infinity, 'unknown']) {
    assert.equal(finite(value), null);
    assert.equal(number(value), '—');
    assert.equal(seconds(value), '—');
    assert.equal(bytes(value), '—');
    assert.equal(percent(value), '—');
  }
  assert.equal(number(0), '0');
  assert.equal(seconds(0), '0 ms');
  assert.equal(bytes(128 * 1024 ** 3), '128 GiB');
  assert.equal(percent(0.825), '82.5%');
});

test('Live Go health objects and active task arrays are rendered semantically', () => {
  assert.equal(healthStatus({ status: 'ok', ranks: [true, true] }), 'ok');
  assert.equal(healthStatus({ status: 'poisoned' }), 'poisoned');
  assert.equal(healthStatus('healthy'), 'healthy');
  assert.equal(healthStatus({}), 'unknown');
  assert.equal(activeRequestLabel([], false), 'Idle');
  assert.equal(activeRequestLabel([], true), 'Active');
  assert.equal(activeRequestLabel(['task1', 'task2'], true), 'task1 · task2');
  assert.equal(activeRequestLabel(undefined, undefined), '—');
});

test('Live CIRU metrics calculate decode from measured generation time, not HTTP TPS', () => {
  const payload = { usage: { completion_tokens: 101 }, metrics: { generation_time_ms: 4000, tokens_per_second: 13 } };
  const delta = completionDelta(payload);
  assert.equal(decodeRate(delta.timings, delta.usage), 25);
  assert.equal(decodeRate({ tokens_per_second: 13 }, payload.usage), null);
  assert.equal(decodeRate({ generation_time_ms: 0 }, payload.usage), null);
  assert.equal(decodeRate({ decode_tps: 24.8 }, payload.usage), 24.8);
});

test('Task status never treats incomplete or merely completed as passing', () => {
  for (const status of ['INCOMPLETE', 'FAILED', 'cancelled', 'completed', 'timeout', 'INTERRUPTED']) {
    assert.ok(isTerminal(status));
    assert.equal(isSuccess(status), false);
  }
  for (const status of ['queued', 'running', 'draining', 'applying', 'APPLYING']) assert.equal(isTerminal(status), false);
  assert.ok(isSuccess('PASS'));
  assert.equal(classifyStatus('unhealthy'), 'bad');
  assert.equal(classifyStatus('running'), 'neutral');
});

test('Applying remains nonterminal but cannot cancel or claim a passing result', () => {
  assert.equal(canCancelTask('applying'), false);
  assert.equal(canCancelTask('APPLYING'), false);
  assert.equal(canCancelTask('draining'), false);
  assert.equal(canCancelTask('PASS'), false);
  assert.equal(canCancelTask(undefined), false);
  assert.equal(canCancelTask('running'), true);
  assert.equal(canCancelTask('QUEUED'), true);
  assert.equal(isSuccess('applying'), false);
  assert.equal(isTerminal('applying'), false);
});

test('finish_reason stop without SSE DONE is not a completed response', () => {
  assert.equal(completionState(false, 'stop', 'apparently final'), 'incomplete-stream');
  assert.equal(completionState(false, 'length', 'partial'), 'incomplete-stream');
  assert.equal(completionState(true, 'length', 'partial'), 'incomplete-cap');
  assert.equal(completionState(true, 'stop', ''), 'no-complete-final');
  assert.equal(completionState(true, 'tool_calls', 'text'), 'no-complete-final');
  assert.equal(completionState(true, 'stop', 'final'), 'complete');
});

test('Task envelopes preserve authoritative top-level status', () => {
  const result = normalizeTask({ task: { id: 't1', status: 'cancelled', result: { status: 'pass', files: { 'a.c': '' }, patch: '+a' } } });
  assert.equal(result.status, 'cancelled');
  assert.equal(result.id, 't1');
  assert.deepEqual(result.files_changed, ['a.c']);
  assert.equal(result.diff, '+a');
  assert.deepEqual(result.attempts, []);
});

test('Path lists deduplicate, preserve path text and cannot become HTML', () => {
  assert.deepEqual(pathList(' src/a.c\ninclude/a.h, src/a.c\n'), ['src/a.c', 'include/a.h']);
  assert.equal(escapeHTML('<img src=x onerror="alert(1)">&\''), '&lt;img src=x onerror=&quot;alert(1)&quot;&gt;&amp;&#39;');
  assert.equal(errorMessage({ error: { message: '<script>x</script>' } }), '<script>x</script>');
  assert.equal(errorMessage({ error: {} }, 'fallback'), 'fallback');
});

test('Task JSON import allows only task fields, never automatic apply or credentials', () => {
  const input = { task: 'Fix a parser', repo: '/repo', allowed_paths: ['src/a.c'], files: ['src/a.c'], test_command: ['./verify'], build_command: ['cc', 'src/a.c', '-o', 'file with space'], profile: 'fast', test_files: { 'verify.c': '/independent/verify.c' }, apply: true, auth: 'secret', endpoint: 'https://example.com', sandbox_policy: 'host', max_tokens: 4096 };
  const spec = importedTaskSpec(input);
  assert.equal(spec.apply, undefined);
  assert.equal(spec.auth, undefined);
  assert.equal(spec.endpoint, undefined);
  assert.equal(spec.sandbox_policy, undefined);
  assert.deepEqual(spec.test_files, { 'verify.c': '/independent/verify.c' });
  assert.equal(spec.build_command, "cc src/a.c -o 'file with space'");
  assert.equal(commandText(['echo', "it's", '']), "echo 'it'\\''s' ''");
  assert.throws(() => importedTaskSpec({ ...input, max_tokens: 0 }), /Invalid max_tokens/);
  assert.throws(() => importedTaskSpec({ ...input, max_tokens: 16385 }), /Invalid max_tokens/);
  assert.throws(() => importedTaskSpec({ ...input, max_repairs: 7 }), /Invalid max_repairs/);
  assert.equal(importedTaskSpec({ ...input, max_repairs: 6 }).max_repairs, 6);
  assert.equal(importedTaskSpec({ ...input, timeout: 1800 }).timeout, 1800);
  assert.throws(() => importedTaskSpec({ ...input, timeout: 1801 }), /Invalid timeout/);
  assert.throws(() => importedTaskSpec({ ...input, timeout: 7200 }), /Invalid timeout/);
  assert.throws(() => importedTaskSpec({ ...input, test_files: [] }), /test_files/);
  assert.throws(() => importedTaskSpec({ ...input, test_command: [1] }), /Commands/);
});

test('Frontend source has no dynamic HTML/eval, persisted token, CDN or external dependency', async () => {
  const script = await readFile(new URL('../app.js', import.meta.url), 'utf8');
  const html = await readFile(new URL('../index.html', import.meta.url), 'utf8');
  assert.doesNotMatch(script, /(?:innerHTML|outerHTML|insertAdjacentHTML|document\.write|\beval\s*\(|new Function|localStorage|sessionStorage)/);
  assert.doesNotMatch(html, /(?:src|href)=["']https?:\/\//);
  assert.match(script, /textContent/);
  assert.match(script, /window\.confirm/);
  assert.match(script, /apply: false/);
  assert.match(script, /credentials: 'omit'/);
  assert.match(html, /id="code-repairs"[^>]*max="6"/);
  assert.match(html, /id="code-timeout"[^>]*max="1800"/);
  assert.match(html, /id="chat-cap"[^>]*max="16384"/);
  assert.match(script, /cap > 16384/);
  assert.doesNotMatch(script, /cap > 32768/);
  assert.match(script, /estimated context-selection budget/);
  assert.match(script, /not a qualified actual-token limit/);
  assert.match(html, /LAST ATTEMPT TPS/);
  assert.match(script, /completionState\(done, finish, text\)/);
  const referencedIDs = [...script.matchAll(/\$\('([^']+)'\)/g)].map(match => match[1]);
  for (const id of referencedIDs) assert.ok(html.includes(`id="${id}"`), `Missing HTML element: ${id}`);
  const definedIDs = [...html.matchAll(/\bid="([^"]+)"/g)].map(match => match[1]);
  assert.equal(new Set(definedIDs).size, definedIDs.length, 'Duplicate HTML IDs');
});

test('Go embeds only the four production frontend assets, not tests or documentation', async () => {
  const source = await readFile(new URL('../../main.go', import.meta.url), 'utf8');
  const match = source.match(/^\/\/go:embed (.+)$/m);
  assert.ok(match, 'Missing explicit production asset embed declaration');
  assert.deepEqual(match[1].trim().split(/\s+/).sort(), ['web/app.js', 'web/index.html', 'web/styles.css', 'web/ui-core.mjs']);
});
