/**
 * DNS rebinding against the Node-side requests (#108): a hostname that answers
 * a public address when the policy checks it and a loopback address when the
 * request connects. Requests to `localhost` are used because the system
 * resolver really answers loopback for it, so a request that skipped the
 * connect-time check would reach the local server; the injected resolver is
 * what the policy sees. Without consent, or for another origin than the
 * approved one, that loopback answer must be refused when the socket connects.
 */
const assert = require('node:assert/strict');
const http = require('node:http');
const test = require('node:test');

const { safeFetch } = require('../dist/network-policy.js');

const PUBLIC = { address: '93.184.215.14', family: 4 };
const LOOPBACK = { address: '127.0.0.1', family: 4 };

/** Public for every odd call (the check), loopback for every even one (the connection). */
function rebindingResolver() {
  let calls = 0;
  const resolver = async () => {
    calls += 1;
    return calls % 2 === 1 ? [PUBLIC] : [LOOPBACK];
  };
  return Object.assign(resolver, { calls: () => calls });
}

/** Always loopback: what `localhost` really is. */
const loopbackResolver = async () => [LOOPBACK];

async function server(t, handler) {
  const hits = [];
  const srv = http.createServer((req, res) => {
    hits.push(req.url);
    handler(req, res);
  });
  await new Promise((resolve) => srv.listen(0, '127.0.0.1', resolve));
  t.after(() => {
    srv.closeAllConnections();
    return new Promise((resolve) => srv.close(resolve));
  });
  return { origin: `http://localhost:${srv.address().port}`, hits };
}

const ok = (_req, res) => res.end('ok');

async function refusal(promise) {
  try {
    await promise;
  } catch (error) {
    return error.message;
  }
  assert.fail('the request was permitted');
}

test('a direct request whose name rebinds to loopback is refused at connect time', async (t) => {
  const target = await server(t, ok);
  const resolver = rebindingResolver();
  const message = await refusal(safeFetch(`${target.origin}/secret`, { method: 'GET' }, { lookup: resolver }));
  assert.match(message, /not permitted by the local safety policy/);
  assert.doesNotMatch(message, /127\.0\.0\.1|93\.184/, 'the resolved addresses are never disclosed');
  assert.deepEqual(target.hits, [], 'the loopback server was never reached');
  assert.equal(resolver.calls(), 2, 'the name was resolved for the check and again for the connection');
});

test('a connect-time refusal reads the same as a check-time refusal', async (t) => {
  const target = await server(t, ok);
  const atConnect = await refusal(safeFetch(`${target.origin}/`, {}, { lookup: rebindingResolver() }));
  const atCheck = await refusal(safeFetch(`${target.origin}/`, {}, { lookup: loopbackResolver }));
  assert.equal(atConnect, atCheck);
  assert.deepEqual(target.hits, []);
});

// The approved origin is an address literal, which is never resolved, so the
// resolver only sees the redirect target: public for its check, loopback for
// its connection.
test('a redirect to a name that rebinds to another loopback origin is refused', async (t) => {
  const other = await server(t, ok);
  const approved = await server(t, (_req, res) => {
    res.writeHead(302, { location: `${other.origin}/internal` });
    res.end();
  });
  const start = approved.origin.replace('localhost', '127.0.0.1');
  const resolver = rebindingResolver();
  const message = await refusal(
    safeFetch(`${start}/start`, { method: 'GET' }, {
      lookup: resolver,
      allowPrivateNetwork: true,
      privateNetworkOrigin: start,
    })
  );
  assert.match(message, /not permitted by the local safety policy/);
  assert.match(message, new RegExp(`only the loopback origin it was given, ${start.replace(/\./g, '\\.')}`));
  assert.equal(resolver.calls(), 2, 'the redirect target was resolved for the check and for the connection');
  // The message names the approved origin the caller gave, never an answer.
  assert.doesNotMatch(message.replace(start, ''), /127\.0\.0\.1|93\.184/);
  assert.deepEqual(approved.hits, ['/start']);
  assert.deepEqual(other.hits, [], 'the redirect target was never reached');
});

test('a redirect without explicit consent scope pins consent to the first origin', async (t) => {
  const other = await server(t, ok);
  const approved = await server(t, (_req, res) => {
    res.writeHead(307, { location: `${other.origin}/internal` });
    res.end();
  });
  const start = approved.origin.replace('localhost', '127.0.0.1');
  await refusal(safeFetch(`${start}/start`, {}, { lookup: rebindingResolver(), allowPrivateNetwork: true }));
  assert.deepEqual(approved.hits, ['/start']);
  assert.deepEqual(other.hits, []);
});

test('loopback consent covers only the approved origin when the name rebinds', async (t) => {
  const approved = await server(t, ok);
  const other = await server(t, ok);
  const consent = { allowPrivateNetwork: true, privateNetworkOrigin: approved.origin };

  const response = await safeFetch(`${approved.origin}/page`, { method: 'GET' }, { ...consent, lookup: rebindingResolver() });
  assert.equal(response.status, 200);
  assert.equal(await response.text(), 'ok');

  const message = await refusal(safeFetch(`${other.origin}/page`, { method: 'GET' }, { ...consent, lookup: rebindingResolver() }));
  assert.match(message, new RegExp(`only the loopback origin it was given, ${approved.origin}`));
  assert.deepEqual(approved.hits, ['/page']);
  assert.deepEqual(other.hits, [], 'the second loopback origin was never reached');
});

test('with consent, redirects within the approved origin are followed and the body is bounded', async (t) => {
  const target = await server(t, (req, res) => {
    if (req.url === '/start') {
      res.writeHead(301, { location: '/final?x=1' });
      res.end();
      return;
    }
    if (req.url === '/big') {
      res.writeHead(200, { 'content-type': 'text/plain', 'content-length': '4096' });
      res.end('x'.repeat(4096));
      return;
    }
    res.writeHead(404, { 'content-type': 'text/plain', 'x-probe': 'yes' });
    res.end('missing');
  });
  const policy = { allowPrivateNetwork: true, privateNetworkOrigin: target.origin, lookup: loopbackResolver };

  const response = await safeFetch(`${target.origin}/start`, { method: 'GET' }, policy);
  assert.equal(response.status, 404);
  assert.equal(response.headers.get('x-probe'), 'yes');
  assert.equal(await response.text(), 'missing');
  assert.deepEqual(target.hits, ['/start', '/final?x=1']);

  const head = await safeFetch(`${target.origin}/final`, { method: 'HEAD' }, policy);
  assert.equal(head.status, 404);
  assert.equal(await head.text(), '');

  await assert.rejects(
    safeFetch(`${target.origin}/big`, {}, { ...policy, maxResponseBytes: 1024 }),
    /exceeded the safety size limit/,
    'a declared length over the limit is refused'
  );
});

test('a streamed body over the limit errors while it is read', async (t) => {
  const target = await server(t, (_req, res) => {
    res.writeHead(200, { 'content-type': 'text/plain' });
    res.write('x'.repeat(800));
    res.end('y'.repeat(800));
  });
  const policy = { allowPrivateNetwork: true, lookup: loopbackResolver, maxResponseBytes: 1024 };
  const response = await safeFetch(`${target.origin}/`, {}, policy);
  assert.equal(response.status, 200);
  await assert.rejects(response.text(), /exceeded the safety size limit/);
});

test('a target that never answers fails after the timeout', async (t) => {
  const target = await server(t, () => {});
  const started = Date.now();
  await assert.rejects(
    safeFetch(`${target.origin}/`, {}, { allowPrivateNetwork: true, lookup: loopbackResolver, timeoutMs: 200 }),
    /^Error: Network request failed\.$/
  );
  assert.ok(Date.now() - started < 5_000);
});

test('too many redirects fail with the redirect limit', async (t) => {
  const target = await server(t, (_req, res) => {
    res.writeHead(302, { location: '/again' });
    res.end();
  });
  await assert.rejects(
    safeFetch(`${target.origin}/`, {}, { allowPrivateNetwork: true, lookup: loopbackResolver, maxRedirects: 2 }),
    /Network redirect limit exceeded/
  );
  assert.equal(target.hits.length, 3);
});
