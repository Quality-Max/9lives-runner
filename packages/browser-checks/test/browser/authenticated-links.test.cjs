const assert = require('node:assert/strict');
const test = require('node:test');
const http = require('node:http');
const path = require('node:path');
const { mkdtemp, rm, writeFile } = require('node:fs/promises');
const { scanUrl } = require('../../dist/scan-url.js');

test('an authenticated scan does not request a link that signs out', async (t) => {
  const requested = [];
  const server = http.createServer((request, response) => {
    requested.push(request.url);
    const signedIn = (request.headers.cookie || '').includes('session=granted');
    if (request.url === '/') {
      response.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
      response.end('<!doctype html><html lang="en"><head><title>Account</title></head><body><main><h1>Account</h1>' +
        '<a href="/orders">Orders</a><a href="/logout">Sign out</a><a href="/orders/7/delete">Delete order</a></main></body></html>');
      return;
    }
    response.writeHead(signedIn ? 200 : 401, { 'content-type': 'text/plain' });
    response.end(signedIn ? 'ok' : 'unauthorized');
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  t.after(() => new Promise((resolve) => server.close(resolve)));
  const { port } = server.address();

  const directory = await mkdtemp(path.join(process.cwd(), '.auth-links-test-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const statePath = path.join(directory, 'state.json');
  await writeFile(statePath, JSON.stringify({
    cookies: [{ name: 'session', value: 'granted', domain: '127.0.0.1', path: '/', expires: -1, httpOnly: true, secure: false, sameSite: 'Lax' }],
    origins: [],
  }));

  const result = await scanUrl({
    url: `http://127.0.0.1:${port}/`,
    checks: ['links'],
    allowPrivateNetwork: true,
    storageStatePath: path.relative(process.cwd(), statePath),
    acknowledgePrivateContent: true,
  });

  assert.ok(requested.includes('/orders'), 'an ordinary protected link is still checked');
  assert.equal(requested.includes('/logout'), false, 'the sign-out link was requested');
  assert.equal(requested.includes('/orders/7/delete'), false, 'the delete link was requested');
  const skipped = result.findings.filter((finding) => finding.severity === 'info' && /signs out or changes data/.test(finding.message));
  assert.deepEqual(skipped.map((finding) => new URL(finding.url).pathname).sort(), ['/logout', '/orders/7/delete']);
  assert.equal(result.findings.some((finding) => finding.severity !== 'info'), false);
});
