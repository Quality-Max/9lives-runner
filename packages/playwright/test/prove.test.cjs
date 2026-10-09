const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {createHash} = require('node:crypto');
const {matches, openProveChannel, proveProtocol, readFault, requestTarget} = require('../dist/prove.js');
const {protocolVersion} = require('../dist/protocol.js');

const request = (url, method = 'GET', resourceType = 'fetch') => ({url: () => url, method: () => method, resourceType: () => resourceType});
const fault = {id: 'fault-1', kind: 'abort', method: 'POST', origin: 'http://127.0.0.1:4100', path: '/api/orders'};

test('prove targets fetch and XHR over http(s) without query strings', () => {
  assert.deepEqual(requestTarget(request('http://127.0.0.1:4100/api/orders?token=secret#x', 'POST')), {method: 'POST', origin: 'http://127.0.0.1:4100', path: '/api/orders'});
  assert.deepEqual(requestTarget(request('https://shop.test/api/cart', 'GET', 'xhr')), {method: 'GET', origin: 'https://shop.test', path: '/api/cart'});
  assert.equal(requestTarget(request('http://shop.test/', 'GET', 'document')), undefined);
  assert.equal(requestTarget(request('http://shop.test/app.js', 'GET', 'script')), undefined);
  assert.equal(requestTarget(request('data:application/json,{}')), undefined);
  assert.equal(requestTarget(request('not a url')), undefined);
});

test('a fault matches only its method, origin and path', () => {
  assert(matches(fault, requestTarget(request('http://127.0.0.1:4100/api/orders?retry=1', 'POST'))));
  assert(!matches(fault, requestTarget(request('http://127.0.0.1:4100/api/orders', 'GET'))));
  assert(!matches(fault, requestTarget(request('http://127.0.0.1:4101/api/orders', 'POST'))));
  assert(!matches(fault, requestTarget(request('http://127.0.0.1:4100/api/orders/1', 'POST'))));
  assert(!matches(fault, undefined));
});

test('the fault schema is closed', () => {
  assert.deepEqual(readFault(JSON.stringify(fault)), fault);
  for (const bad of [
    'not json', '[]', 'null',
    JSON.stringify({...fault, extra: 1}),
    JSON.stringify({...fault, kind: 'delay'}),
    JSON.stringify({...fault, id: 'fault-0'}),
    JSON.stringify({...fault, method: 'post'}),
    JSON.stringify({...fault, origin: 'http://user@host'}),
    JSON.stringify({...fault, path: '/api?x=1'}),
    JSON.stringify({id: fault.id, kind: fault.kind, method: fault.method, origin: fault.origin}),
  ]) {
    assert.throws(() => readFault(bad), /invalid fault/, bad);
  }
});

test('the prove channel is off outside 9l prove and exclusive per attempt inside it', () => {
  const info = {testId: 'abc', retry: 0};
  assert.equal(openProveChannel({}, info), undefined);
  assert.throws(() => openProveChannel({NINELIVES_PROVE: '9l.prove/0'}, info), /supported Go engine/);
  const identity = {NINELIVES_ENGINE_PROTOCOL: protocolVersion, NINELIVES_RUN_ID: 'run-1', NINELIVES_JOB_ID: 'job-001', NINELIVES_ATTEMPT_ID: 'job-001-attempt-001'};
  assert.throws(() => openProveChannel({NINELIVES_PROVE: proveProtocol}, info), /attempt identity|Go engine/);
  assert.throws(() => openProveChannel({...identity, NINELIVES_PROVE: proveProtocol, NINELIVES_PROVE_DIR: 'relative'}, info), /evidence directory/);
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), '9l-prove-test-'));
  try {
    const env = {...identity, NINELIVES_PROVE: proveProtocol, NINELIVES_PROVE_DIR: dir, NINELIVES_PROVE_FAULT: JSON.stringify(fault)};
    const channel = openProveChannel(env, info);
    assert.equal(channel.fault.id, 'fault-1');
    channel.record({type: 'applied', fault: 'fault-1'});
    channel.record({type: 'x'.repeat(5000)});
    channel.record({type: 'applied', fault: 'fault-1'});
    channel.close();
    channel.record({type: 'applied', fault: 'fault-1'});
    assert.throws(() => openProveChannel(env, info), /EEXIST/);
    const [file] = fs.readdirSync(dir);
    assert.match(file, /^[a-f0-9]{32}\.ndjson$/);
    // Windows inherits directory ACLs; its mode bits cannot express POSIX 0600.
    if (process.platform !== 'win32') assert.equal(fs.statSync(path.join(dir, file)).mode & 0o777, 0o600);
    // The handshake carries the engine's hashed test ID and the retry index;
    // an oversized record ends the stream with an overflow marker.
    const testId = createHash('sha256').update('abc').digest('hex');
    assert.deepEqual(fs.readFileSync(path.join(dir, file), 'utf8').trim().split('\n').map(line => JSON.parse(line)), [
      {type: 'hello', protocol: proveProtocol, mode: 'fault', testId, retry: 0}, {type: 'applied', fault: 'fault-1'}, {type: 'overflow'},
    ]);
    assert.throws(() => openProveChannel({...env, NINELIVES_PROVE_FAULT: '{}'}, {testId: 'other', retry: 0}), /invalid fault/);
  } finally {
    fs.rmSync(dir, {recursive: true, force: true});
  }
});

test('a fault is credited only after Playwright delivers it', async () => {
  const {instrument, ProveChannel} = require('../dist/prove.js');
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), '9l-prove-route-'));
  try {
    for (const [kind, settle, credited] of [['abort', 'resolve', 1], ['abort', 'reject', 0], ['http-500', 'reject', 0]]) {
      const file = path.join(dir, `${kind}-${settle}.ndjson`);
      const channel = new ProveChannel(fs.openSync(file, 'wx', 0o600), {...fault, kind});
      let handler;
      await instrument({route: async (_pattern, h) => { handler = h; }}, channel);
      const outcome = () => settle === 'resolve' ? Promise.resolve() : Promise.reject(new Error('request already handled'));
      const route = {request: () => request('http://127.0.0.1:4100/api/orders', 'POST'), abort: outcome, fulfill: outcome, fallback: () => Promise.resolve()};
      await handler(route).catch(() => {});
      channel.close();
      const applied = fs.readFileSync(file, 'utf8').split('\n').filter(line => line.includes('"applied"')).length;
      assert.equal(applied, credited, `${kind} ${settle}`);
    }
  } finally {
    fs.rmSync(dir, {recursive: true, force: true});
  }
});

test('empty-json replaces a JSON body, says why it could not, and leaves other requests to the test', async () => {
  const {instrument, ProveChannel} = require('../dist/prove.js');
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), '9l-prove-empty-'));
  try {
    const cases = [
      ['array', {text: '[{"name":"Book"}]'}, [{type: 'applied', fault: 'fault-1'}], [{body: '[]'}]],
      ['object', {text: '{"count":2}'}, [{type: 'applied', fault: 'fault-1'}], [{body: '{}'}]],
      ['scalar', {text: '"ok"'}, [{type: 'not-applicable', fault: 'fault-1', reason: 'not-json'}], [{}]],
      ['not json', {text: '<html>'}, [{type: 'not-applicable', fault: 'fault-1', reason: 'not-json'}], [{}]],
      ['unreachable', {fetchFails: true}, [{type: 'not-applicable', fault: 'fault-1', reason: 'unreachable'}], []],
    ];
    for (const [name, upstream, records, fulfilled] of cases) {
      const file = path.join(dir, `${name.replace(' ', '-')}.ndjson`);
      const channel = new ProveChannel(fs.openSync(file, 'wx', 0o600), {...fault, kind: 'empty-json'});
      let handler;
      await instrument({route: async (_pattern, h) => { handler = h; }}, channel);
      const calls = {fulfill: [], continued: 0, fallback: 0};
      const response = {text: async () => upstream.text};
      const route = {
        request: () => request('http://127.0.0.1:4100/api/orders', 'POST'),
        fetch: async () => { if (upstream.fetchFails) throw new Error('ECONNREFUSED'); return response; },
        fulfill: async options => { calls.fulfill.push(options.body === undefined ? {} : {body: options.body}); },
        continue: async () => { calls.continued++; },
        fallback: async () => { calls.fallback++; },
      };
      await handler(route);
      channel.close();
      // A channel built directly writes no handshake, so every line is a record.
      const written = fs.readFileSync(file, 'utf8').split('\n').filter(Boolean).map(line => JSON.parse(line));
      assert.deepEqual(written, records, name);
      assert.deepEqual(calls.fulfill, fulfilled, name);
      assert.equal(calls.continued, upstream.fetchFails ? 1 : 0, name);
      assert.equal(calls.fallback, 0, name);
    }
    // A request the fault does not match is handed to the test's own routes.
    const file = path.join(dir, 'other.ndjson');
    const channel = new ProveChannel(fs.openSync(file, 'wx', 0o600), {...fault, kind: 'empty-json'});
    let handler;
    await instrument({route: async (_pattern, h) => { handler = h; }}, channel);
    let fallback = 0;
    await handler({request: () => request('http://127.0.0.1:4100/api/cart', 'GET'), fallback: async () => { fallback++; }});
    channel.close();
    assert.equal(fallback, 1);
    assert.equal(fs.readFileSync(file, 'utf8'), '');
  } finally {
    fs.rmSync(dir, {recursive: true, force: true});
  }
});
