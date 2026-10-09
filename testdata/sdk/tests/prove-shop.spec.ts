import {createServer, type Server} from 'node:http';
import type {AddressInfo} from 'node:net';
import {test as base, expect} from '@9l/playwright';

// Synthetic local shop for `9l prove`: no real order, account or external
// server. The cart and order responses are asserted; recommendations render
// but are deliberately never asserted, so their faults must survive.
const html = `<!doctype html>
<ul aria-label="Cart"></ul><p role="status">Loading</p>
<button type="button">Place order</button><aside aria-label="Recommended"></aside>
<script>
  const status = document.querySelector('[role=status]');
  fetch('/api/cart').then(r => r.ok ? r.json() : []).then(items => {
    for (const item of items) document.querySelector('ul').append(Object.assign(document.createElement('li'), {textContent: item.name}));
    status.textContent = 'Cart ready';
  }, () => { status.textContent = 'Cart unavailable'; });
  fetch('/api/recommendations').then(r => r.json()).then(items => {
    document.querySelector('aside').textContent = items.map(item => item.name).join(', ');
  }).catch(() => {});
  document.querySelector('button').onclick = async () => {
    try {
      const response = await fetch('/api/orders', {method: 'POST', headers: {'content-type': 'application/json'}, body: '{"items":2}'});
      if (!response.ok) throw new Error('order rejected');
      const order = await response.json();
      status.textContent = 'Order confirmed: ' + order.count + ' items';
    } catch {
      status.textContent = 'Order failed';
    }
  };
</script>`;

const test = base.extend<{}, {shop: string}>({
  shop: [async ({}, use) => {
    const server: Server = createServer((request, response) => {
      const json = (value: unknown) => response.writeHead(200, {'content-type': 'application/json'}).end(JSON.stringify(value));
      if (request.url === '/') return response.writeHead(200, {'content-type': 'text/html'}).end(html);
      if (request.url === '/api/cart') return json([{name: 'Book'}, {name: 'Pen'}]);
      if (request.url === '/api/recommendations') return json([{name: 'Notebook'}]);
      if (request.url === '/api/orders' && request.method === 'POST') return json({count: 2});
      response.writeHead(404).end();
    });
    // Faults target the baseline's origin, so every run must use the same port.
    const port = Number(process.env.NINELIVES_PROVE_SHOP_PORT ?? 0);
    await new Promise<void>(resolve => server.listen(port, '127.0.0.1', resolve));
    await use(`http://127.0.0.1:${(server.address() as AddressInfo).port}/`);
    await new Promise(resolve => server.close(resolve));
  }, {scope: 'worker'}],
});
const check = expect.configure({timeout: 2000});

test('checkout confirms an order for the cart', async ({page, shop}) => {
  await page.goto(shop);
  await check(page.getByRole('list', {name: 'Cart'}).getByRole('listitem')).toHaveCount(2);
  await page.getByRole('button', {name: 'Place order'}).click();
  await check(page.getByRole('status')).toHaveText('Order confirmed: 2 items');
});
