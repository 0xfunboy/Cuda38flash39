// Opt-in, one small actual Pi task. Never imported by make test/CI.
// Usage: node benchmarks/product007-smoke.mjs --run-live /new/report/directory [baseURL] [tokenFile]
// This tests integration, not model intelligence, performance or remote SSH.
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import assert from 'node:assert/strict';

const [flag, rawReport, base = 'http://127.0.0.1:18093', tokenFile = '/home/funboy/Cuda38flash39/state/api-token'] = process.argv.slice(2);
if (flag !== '--run-live' || !rawReport) throw Error('Explicit --run-live and a NEW report directory are required; this makes actual model calls.');
const report = path.resolve(rawReport);
const evidenceRoot = '/home/funboy/ai-exp/reports/moe-cluster/';
if (!report.startsWith(evidenceRoot) || report === evidenceRoot.slice(0, -1)) throw Error('Use a new bounded subdirectory of the existing report root.');
if (fs.existsSync(report)) throw Error('Report directory already exists; inspect it, do not replay automatically.');
const endpoint = new URL(base);
if (endpoint.protocol !== 'http:' || endpoint.hostname !== '127.0.0.1') throw Error('Only an explicitly selected loopback gateway is supported.');
const token = fs.readFileSync(tokenFile, 'utf8').trim();
if (!token) throw Error('Missing local credential.');
fs.mkdirSync(report, { recursive: true, mode: 0o700 });
const project = path.join(report, 'original-project');
fs.mkdirSync(project, { mode: 0o700 });
const original = '#include <stdio.h>\nstatic int add(int a, int b) { return a - b; }\nint main(void) { printf("%d\\n", add(12, 30)); return 0; }\n';
fs.writeFileSync(path.join(project, 'main.c'), original, { mode: 0o600, flag: 'wx' });
fs.writeFileSync(path.join(project, 'keep.md'), 'Pre-existing untracked fixture note; must remain unchanged.\n', { mode: 0o600, flag: 'wx' });
const hash = text => crypto.createHash('sha256').update(text).digest('hex');
const result = { status: 'RUNNING', scope: 'One local protected Pi integration task; no performance/intelligence claim', started: new Date().toISOString(), project, original_sha256: hash(original), checks: [], requests: 0 };
let workspaceID, conversationID, closed = false;
function save() { fs.writeFileSync(path.join(report, 'summary.json'), JSON.stringify(result, null, 2) + '\n', { mode: 0o600 }); }
async function api(route, method = 'GET', body, timeout = 20000) {
  result.requests++;
  const response = await fetch(base + route, { method, headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(timeout) });
  const data = await response.json();
  if (!response.ok) throw Error(`${method} ${route}: HTTP ${response.status}: ${String(data.error || 'request failed')}`);
  return data;
}
function check(label, condition) { assert.ok(condition, label); result.checks.push(label); save(); }
save();
try {
  let conversation = await api('/v1/conversations', 'POST', { title: 'Protected Pi integration smoke' });
  conversationID = conversation.id; result.conversation_id = conversationID; save();
  const message = { id: crypto.randomBytes(16).toString('hex'), role: 'user', origin: 'chat', status: 'complete', content: 'HALOCLU_HANDOFF_SUM: In main.c, add(a,b) must return the mathematical sum. Fix its incorrect subtraction so the unchanged main prints 42. Modify main.c only. Do not change keep.md or introduce files, install packages, use network or Git.' };
  conversation = await api(`/v1/conversations/${conversationID}`, 'PUT', { revision: conversation.revision, title: conversation.title, messages: [message] });
  const session = await api('/v1/workspaces/sessions', 'POST', { kind: 'local', root: project, mode: 'protected', reasoning_effort: 'low', conversation_id: conversationID, build_command: ['gcc', '-std=c11', '-Wall', '-Wextra', '-Werror', 'main.c', '-o', 'sum'], test_command: ['/bin/sh', '-c', 'test "$(./sum)" = 42'], confirm: true });
  workspaceID = session.id; result.workspace_id = workspaceID; result.working_root = session.working_root; save();
  check('Protected copy selected; original and working roots differ', session.mode === 'protected' && session.original_root === project && session.working_root !== project);
  conversation = await api(`/v1/conversations/${conversationID}`);
  const handoff = await api(`/v1/conversations/${conversationID}/handoff`, 'POST', { workspace_id: workspaceID, revision: conversation.revision, confirm: true });
  check('Canonical transcript staged without inference/tool replay', handoff.executed === false && handoff.mode === 'transcript_context');
  const started = await api(`/v1/workspaces/sessions/${workspaceID}/start`, 'POST', { confirm: true }, 30000);
  check('Actual Pi starts ready inside the owned scope', started.state === 'READY' && typeof started.scope_invocation === 'string' && started.scope_invocation.length > 0);
  const generationStart = performance.now();
  await api(`/v1/workspaces/sessions/${workspaceID}/prompt`, 'POST', { confirm: true, message: 'Implement the request in the transferred HALOCLU_HANDOFF_SUM conversation. Read main.c, make only the required fix, optionally compile/test, and finish with a short honest summary. At most four tool calls. Do not modify any other file.' });
  const deadline = Date.now() + 180000;
  let current;
  while (Date.now() < deadline) {
    await new Promise(resolve => setTimeout(resolve, 1500));
    current = await api(`/v1/workspaces/sessions/${workspaceID}`);
    if (current.verification && current.verification.origin === 'agent_end' && current.verification.status !== 'VERIFYING' && !['RUNNING', 'VERIFYING'].includes(current.state)) break;
    if (['FAILED', 'CLOSED'].includes(current.state)) throw Error(`Pi ended in ${current.state}: ${current.blocked_reason || ''}`);
  }
  result.pi_and_verification_seconds = (performance.now() - generationStart) / 1000;
  result.verification = current?.verification || null; save();
  check('Agent concludes naturally and independent commands pass', current?.verification?.status === 'TEST_PASS' && current.verification.completion === 'NATURAL' && current.verification.build?.passed && current.verification.tests?.passed);
  check('Original source stays unchanged before Apply', fs.readFileSync(path.join(project, 'main.c'), 'utf8') === original);
  const review = await api(`/v1/workspaces/sessions/${workspaceID}/review`);
  check('Only the intended source is changed and no tests were weakened', JSON.stringify(review.files_changed) === JSON.stringify(['main.c']) && review.deleted.length === 0 && !(review.verification.changed_test_definitions || []).length);
  check('Review is pinned to the independently checked version', review.candidate_sha256 === current.verification.candidate_sha256);
  const applied = await api(`/v1/workspaces/sessions/${workspaceID}/apply`, 'POST', { confirm: true });
  check('Explicit fixture-only Apply succeeds with receipt', applied.verification.applied === true);
  check('Applied original exactly matches the verified artifact hash', hash(fs.readFileSync(path.join(project, 'main.c'))) === current.verification.file_sha256['main.c']);
  check('Pre-existing untracked fixture remains intact', fs.readFileSync(path.join(project, 'keep.md'), 'utf8') === 'Pre-existing untracked fixture note; must remain unchanged.\n');
  const ended = await api(`/v1/workspaces/sessions/${workspaceID}/close`, 'POST', { confirm: true }, 70000);
  closed = ended.state === 'CLOSED' && ended.closed_cleanly === true;
  check('Owned Pi process scope closed cleanly', closed);
  conversation = await api(`/v1/conversations/${conversationID}`);
  check('Pi output is present in the shared canonical conversation', conversation.messages.some(item => item.origin === 'pi' && item.role === 'assistant'));
  const deleted = await api(`/v1/conversations/${conversationID}`, 'DELETE', { revision: conversation.revision, confirm: true });
  result.deletion = deleted;
  const list = await api('/v1/conversations');
  check('Explicit deletion removes canonical history from the API', !list.conversations.some(item => item.id === conversationID));
  const sessionDirectory = path.dirname(session.working_root);
  check('Owned session files are removed after clean close + deletion', !fs.existsSync(sessionDirectory));
  check('Deleting history does not delete the original fixture project', fs.existsSync(path.join(project, 'main.c')));
  result.status = 'PASS';
} catch (error) {
  result.status = 'FAIL'; result.error = String(error.message).split(token).join('[REDACTED]');
  process.exitCode = 1;
} finally {
  if (workspaceID && !closed) {
    try { const value = await api(`/v1/workspaces/sessions/${workspaceID}/close`, 'POST', { confirm: true }, 70000); closed = value.state === 'CLOSED' && value.closed_cleanly === true; result.cleanup_closed = closed; }
    catch (error) { result.cleanup_error = String(error.message).split(token).join('[REDACTED]'); result.resume_action = `Inspect and close workspace ${workspaceID}; do not replay model prompts. Conversation ${conversationID} retained for diagnosis.`; }
  }
  result.finished = new Date().toISOString(); save();
  process.stdout.write(JSON.stringify({ status: result.status, checks: result.checks.length, summary: path.join(report, 'summary.json'), workspace_id: workspaceID, cleanup_closed: closed }) + '\n');
}
