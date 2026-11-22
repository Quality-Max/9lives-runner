// A one-order shop for the --verify smoke. With NINELIVES_SMOKE_DEFECT=1 the
// order is never stored as confirmed, while the page still says it was.
const http = require('node:http');
const fs = require('node:fs');
const order = {id: 1, status: 'cart', items: 1};
const page = `<!doctype html><button type="button">Place order</button><p role="status">Cart: 1 item</p><script>
document.querySelector('button').addEventListener('click', async () => {
  await fetch('/api/orders/1/confirm', {method: 'POST'});
  document.querySelector('[role=status]').textContent = 'Order confirmed: 1 item';
});</script>`;
const server = http.createServer((request, response) => {
  const route = request.url.split('?')[0];
  if (request.method === 'POST' && route === '/api/orders/1/confirm') {
    if (process.env.NINELIVES_SMOKE_DEFECT !== '1') order.status = 'confirmed';
    response.writeHead(204).end();
  } else if (route === '/api/orders/1') {
    response.writeHead(200, {'content-type': 'application/json'}).end(JSON.stringify(order));
  } else if (route === '/') {
    response.writeHead(200, {'content-type': 'text/html'}).end(page);
  } else {
    response.writeHead(404).end();
  }
});
server.listen(0, '127.0.0.1', () => {
  const marker = process.env.NINELIVES_SMOKE_MARKER;
  fs.writeFileSync(`${marker}.tmp`, JSON.stringify({port: server.address().port}));
  fs.renameSync(`${marker}.tmp`, marker);
});
