// Pi runs in its own network namespace. This internal loopback listener can
// forward only to the gateway's mounted, per-session Unix generation socket.
// No host TCP endpoint (including either inference rank) is exposed.
import http from 'node:http';
import fs from 'node:fs';
import { spawn } from 'node:child_process';

const server = http.createServer((request, response) => {
  if (!['/v1/models', '/v1/chat/completions'].includes(request.url)) {
    response.writeHead(403); response.end('session capability path refused'); return;
  }
  const upstream = http.request({socketPath:'/pi-bridge/api.sock',path:request.url,method:request.method,headers:request.headers}, result => {
    response.writeHead(result.statusCode || 502, result.headers); result.pipe(response);
  });
  upstream.on('error', () => { if (!response.headersSent) response.writeHead(502); response.end('session bridge unavailable'); });
  request.on('aborted', () => upstream.destroy());
  response.on('close', () => { if (!response.writableEnded) upstream.destroy(); });
  request.pipe(upstream);
});
await new Promise((resolve,reject) => {server.once('error',reject);server.listen(0,'127.0.0.1',resolve);});
const file = '/pi-config/models.json';
const models = JSON.parse(fs.readFileSync(file,'utf8'));
models.providers.strixglm.baseUrl = `http://127.0.0.1:${server.address().port}/v1`;
fs.writeFileSync(file,JSON.stringify(models,null,2)+'\n',{mode:0o600});
const child = spawn('/usr/bin/node',['/pi-runtime/node_modules/@earendil-works/pi-coding-agent/dist/bundle/cli.js',...process.argv.slice(2)],{stdio:'inherit',env:process.env});
for (const signal of ['SIGTERM','SIGINT']) process.on(signal, () => {child.kill(signal);});
child.on('error', error => {console.error('Pi startup failed:',error.message);server.close();process.exitCode=1;});
child.on('exit', (code,signal) => {server.closeAllConnections();server.close();process.exitCode=code ?? (signal ? 1 : 0);});
