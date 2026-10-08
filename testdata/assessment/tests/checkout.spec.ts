import { test, expect } from '@playwright/test';

// Synthetic local service: no real order, account, provider or external server.
const selectedItems = [{ sku: 'book', quantity: 2 }, { sku: 'pen', quantity: 1 }];
let orders: { items: typeof selectedItems }[];

test.beforeEach(async ({ page }) => {
  orders = [];
  const control = process.env.NINELIVES_ASSESS_CONTROL;
  if (control === 'setup-failure') throw new Error('Controlled fixture setup failure');
  if (control === 'timeout') {
    test.setTimeout(500);
    await new Promise(() => {});
  }
  await page.route('http://checkout.test/**', async route => {
    if (route.request().method() === 'POST') {
      if (control !== 'missing-order') orders.push({ items: route.request().postDataJSON().items });
      await route.fulfill({ json: { success: true } });
    } else {
      await route.fulfill({ contentType: 'text/html', body: `
        <button>Complete checkout</button><div role="status"></div>
        <script>
          document.querySelector('button').onclick = async () => {
            await fetch('/orders', {method: 'POST', headers: {'Content-Type': 'application/json'},
              body: JSON.stringify({items: ${JSON.stringify(selectedItems)}})});
            document.querySelector('[role="status"]').textContent = 'Order confirmed';
          };
        </script>` });
    }
  });
  await page.goto('http://checkout.test/');
});

// @9l-requirement checkout-order
test('banner misses order creation', async ({ page }) => {
  await page.getByRole('button', { name: 'Complete checkout' }).click();
  // @9l-outcome confirmation
  await expect(page.getByRole('status')).toHaveText('Order confirmed');
});

// @9l-requirement confirmation
test('banner protects confirmation', async ({ page }) => {
  await page.getByRole('button', { name: 'Complete checkout' }).click();
  // @9l-outcome confirmation
  await expect(page.getByRole('status')).toHaveText('Order confirmed');
});

// @9l-requirement checkout-order
test('checks persisted order count and items', async ({ page }) => {
  await page.getByRole('button', { name: 'Complete checkout' }).click();
  await expect(page.getByRole('status')).toHaveText('Order confirmed');
  // @9l-outcome order-count
  expect(orders).toHaveLength(1);
  // @9l-outcome order-items
  expect(orders[0].items).toEqual(selectedItems);
});
