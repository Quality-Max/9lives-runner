/**
 * Redirects inside the browser (#108). Chromium follows a redirect without
 * calling the context's route handler again, so before the policy proxy a
 * navigation, subresource, in-page fetch or same-origin link check that
 * redirected to another loopback origin reached it. The page is served from an
 * approved loopback origin; the "internal" server is a different loopback
 * origin, which the policy refuses, and must never receive a request.
 */
const assert = require('node:assert/strict');
const http = require('node:http');
const test = require('node:test');

const { scanUrl } = require('../../dist/scan-url.js');
const { inspectPage } = require('../../dist/inspect-page.js');

async function listen(t, handler) {
  const hits = [];
  const server = http.createServer((request, response) => {
    hits.push(request.url);
    handler(request, response);
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  t.after(() => {
    server.closeAllConnections();
    return new Promise((resolve) => server.close(resolve));
  });
  return { port: server.address().port, hits };
}

async function fixture(t) {
  const internal = await listen(t, (_request, response) => {
    response.writeHead(200, { 'content-type': 'text/html' });
    response.end('<!doctype html><title>internal</title><h1>internal secret</h1>');
  });
  // `localhost` rather than 127.0.0.1: a different origin from the approved one.
  const internalOrigin = `http://localhost:${internal.port}`;
  const app = await listen(t, (request, response) => {
    if (request.url.startsWith('/away')) {
      response.writeHead(302, { location: `${internalOrigin}${request.url}` });
      response.end();
      return;
    }
    if (request.url === '/same') {
      response.writeHead(302, { location: '/landing' });
      response.end();
      return;
    }
    response.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
    response.end(
      '<!doctype html><html lang="en"><head><title>App</title></head><body><main><h1>App</h1>' +
        '<img src="/away-img" alt="">' +
        '<a href="/away-link">Away</a><a href="/same">Same</a>' +
        "<script>fetch('/away-fetch').catch(() => {});</script>" +
        '</main></body></html>'
    );
  });
  return { appOrigin: `http://127.0.0.1:${app.port}`, app, internal };
}

test('a scan never reaches another loopback origin through a redirect', async (t) => {
  const { appOrigin, app, internal } = await fixture(t);

  await scanUrl({ url: `${appOrigin}/`, checks: ['console', 'links'], allowPrivateNetwork: true });

  assert.deepEqual(internal.hits, [], 'the redirect target on another loopback origin was requested');
  assert.ok(app.hits.includes('/away-img') && app.hits.includes('/away-fetch'), 'the page loaded its subresources');
  assert.ok(app.hits.includes('/landing'), 'a redirect within the approved origin is still followed');
});

test('a navigation that redirects to another loopback origin is not loaded', async (t) => {
  const { appOrigin, internal } = await fixture(t);

  await assert.rejects(inspectPage({ url: `${appOrigin}/away-nav`, allowPrivateNetwork: true }));
  assert.deepEqual(internal.hits, [], 'the redirected navigation reached another loopback origin');
});
