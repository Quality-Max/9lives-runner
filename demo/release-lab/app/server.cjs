const {createServer} = require('node:http');
const {readFileSync} = require('node:fs');
const {join} = require('node:path');
const {countItems} = require('./order.cjs');

// Synthetic data only. Bind to loopback; no account, payment or outbound request.
exports.start = async ({port = 0, quality = 'clean', profileBug = false} = {}) => {
  const items = [{name: 'Field notebook', price: 18}, {name: 'Studio mug', price: 24}];
  const template = readFileSync(join(__dirname, 'index.html'), 'utf8');
  const server = createServer((request, response) => {
    const json = value => response.writeHead(200, {'content-type': 'application/json'}).end(JSON.stringify(value));
    response.setHeader('cache-control', 'no-store');
    response.setHeader('x-content-type-options', 'nosniff');
    response.setHeader('referrer-policy', 'no-referrer');
    response.setHeader('x-frame-options', 'DENY');
    const pathname = new URL(request.url, 'http://localhost').pathname;
    if (pathname === '/') {
      return response.writeHead(200, {'content-type': 'text/html; charset=utf-8'}).end(template
        .replace('/* PROFILE_BUG */ false', String(profileBug))
        .replace('<!-- QUALITY -->', quality === 'broken' ? '<input id="promo" placeholder="Promo code">' : '<label for="promo">Promo code</label><input id="promo" placeholder="Optional">'));
    }
    if (pathname === '/api/cart') return json(items);
    if (pathname === '/api/recommendations') return json([{name: 'Pencil set'}]);
    if (pathname === '/api/orders' && request.method === 'POST') return json({count: countItems(items)});
    if (pathname === '/help' && quality === 'clean') return response.writeHead(200, {'content-type': 'text/html'}).end('<h1>Studio support</h1>All orders here are pretend.');
    if (pathname === '/favicon.ico') return response.writeHead(204).end();
    response.writeHead(404).end('Demo page not found');
  });
  await new Promise((resolve, reject) => { server.once('error', reject); server.listen(port, '127.0.0.1', resolve); });
  return {
    url: `http://127.0.0.1:${server.address().port}/`,
    setQuality: value => { quality = value; },
    close: () => new Promise((resolve, reject) => {
      server.close(error => error ? reject(error) : resolve());
      server.closeAllConnections();
    }),
  };
};

if (require.main === module) {
  exports.start({port: Number(process.argv[2] || 0)}).then(app => {
    console.log(`9l Studio: ${app.url} (local synthetic shop)`);
    for (const signal of ['SIGINT', 'SIGTERM']) process.once(signal, () => app.close().then(() => process.exit(0)));
  }).catch(() => { console.error('Could not start the local demo server.'); process.exitCode = 1; });
}
