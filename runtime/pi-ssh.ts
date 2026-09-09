/**
 * Explicit SSH tool transport for pinned Pi 0.85.1. Based on the upstream MIT
 * example packages/coding-agent/examples/extensions/ssh.ts (d981de1229ef899957bbe968bc8dcda02a21f477).
 * Pi remains the agent; only its read/edit/write/bash operations are delegated.
 * Remote bash runs as the selected account, not in a claimed OS sandbox.
 */
import { spawn } from 'node:child_process';
import path from 'node:path';
import { createReadTool, createEditTool, createWriteTool, createBashTool } from '@earendil-works/pi-coding-agent';

const quote = (s: string) => "'" + s.replaceAll("'", "'\"'\"'") + "'";
const args: string[] = JSON.parse(process.env.STRIXGLM_SSH_ARGS || '[]');
const remoteRoot = process.env.STRIXGLM_REMOTE_ROOT || '';
if (!args.length || !remoteRoot.startsWith('/')) throw new Error('explicit SSH workspace configuration required');
const localRoot = process.cwd();
function remotePath(p: string) {
  if (path.posix.isAbsolute(p) && (p === remoteRoot || p.startsWith(remoteRoot + '/'))) return p;
  const rel = path.relative(localRoot, path.resolve(p));
  if (rel === '..' || rel.startsWith('../') || path.isAbsolute(rel)) throw new Error('path outside selected remote workspace');
  return path.posix.join(remoteRoot, rel);
}
function guard(p: string, missing = false) {
  return `p=$(realpath ${missing ? '-m' : '-e'} -- ${quote(remotePath(p))}) && case "$p" in ${quote(remoteRoot)}|${quote(remoteRoot)}/*) ;; *) exit 73;; esac && `;
}
function run(command: string, input?: string, options?: any): Promise<any> {
  return new Promise((resolve, reject) => {
    const child = spawn('/usr/bin/ssh', [...args, 'bash -c ' + quote(command)], {stdio:['pipe','pipe','pipe']});
    const chunks: Buffer[] = []; let length = 0; let expired = false;
    const limit = 2 * 1024 * 1024;
    const collect = (data: Buffer) => { length += data.length; if (length > limit) {child.kill(); return;} if (options?.onData) options.onData(data); else chunks.push(data); };
    child.stdout.on('data', collect); child.stderr.on('data', collect);
    child.stdin.on('error', () => {}); child.stdin.end(input);
    const abort = () => child.kill(); options?.signal?.addEventListener('abort', abort, {once:true});
    const timer = setTimeout(() => {expired = true; child.kill();}, Math.min(options?.timeout || 120, 300) * 1000);
    child.on('error', reject);
    child.on('close', code => {
      clearTimeout(timer); options?.signal?.removeEventListener('abort', abort);
      if (options?.signal?.aborted) return reject(new Error('aborted; SSH channel closed, remote child termination is not guaranteed'));
      if (expired) return reject(new Error('remote command timeout; remote child termination is not guaranteed'));
      if (length > limit) return reject(new Error('remote output exceeded 2 MiB'));
      if (options) return resolve({exitCode:code});
      if (code !== 0) return reject(new Error(`SSH tool failed (${code}): ${Buffer.concat(chunks).toString()}`));
      resolve(Buffer.concat(chunks));
    });
  });
}
export default function(pi: any) {
  const read = {
    readFile: (p: string) => run(guard(p) + 'cat -- "$p"'),
    access: (p: string) => run(guard(p) + 'test -r "$p"').then(() => {}),
    detectImageMimeType: async () => null,
  };
  const write = {
    writeFile: (p: string, content: string) => run(guard(p,true) + 'test ! -L "$p" && cat > "$p"', content).then(() => {}),
    mkdir: (p: string) => run(guard(p,true) + 'mkdir -p -- "$p"').then(() => {}),
  };
  const bash = {exec: (command: string, cwd: string, opts: any) => run(guard(cwd) + 'cd -- "$p" && ' + command, undefined, opts)};
  for (const tool of [createReadTool(localRoot,{operations:read}),createWriteTool(localRoot,{operations:write}),createEditTool(localRoot,{operations:{...read,...write}}),createBashTool(localRoot,{operations:bash})]) pi.registerTool(tool);
  pi.on('user_bash', () => ({operations:bash}));
  pi.on('before_agent_start', (event: any) => ({systemPrompt:event.systemPrompt.replace(`Current working directory: ${localRoot}`,`Current working directory: ${remoteRoot} (explicit SSH workspace; bash executes as remote user)`)}));
}
