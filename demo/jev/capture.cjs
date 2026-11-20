// Invoked by the Go demo with a filtered environment and the embedded fixture.
const { chromium, expect } = require('@playwright/test');
const path = require('node:path');

(async () => {
  const chunks = [];
  for await (const chunk of process.stdin) chunks.push(chunk);
  const html = Buffer.concat(chunks).toString('utf8');
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 850, height: 600 }, deviceScaleFactor: 1 });
    // The fixed local fixture has no external resources. Block network anyway.
    await page.route('**/*', route => route.abort());
    await page.setContent(html);
    const scenario = await page.locator('body').getAttribute('data-scenario');
    const success = scenario === 'success';
    if (!['declined', 'success'].includes(scenario)) throw new Error('Unknown fixture');
    const readOrders = () => page.evaluate(() => window.demoOrderStore.list());
    await expect.poll(readOrders).toEqual([]);
    await page.getByRole('button', { name: 'Place order', exact: true }).click();
    const expectedOrder = { id: 'demo-order-1', sku: 'field-notebook', quantity: 1, totalCents: 1200, currency: 'EUR' };
    let assertionFailed = false;
    let assertionPassed = false;
    try {
      // Check what the shop saved, independently of any confirmation wording.
      await expect.poll(readOrders, { timeout: 400 }).toEqual([expectedOrder]);
      assertionPassed = true;
    } catch (error) {
      if (!error.matcherResult) throw error;
      assertionFailed = true;
    }
    if (assertionPassed !== success || assertionFailed === success) throw new Error('Unexpected assertion outcome');
    const orders = await readOrders();
    await expect(orders).toEqual(success ? [expectedOrder] : []);
    const retry = page.getByRole('button', { name: success ? 'Start again' : 'Try again', exact: true });
    await expect(retry).toHaveCount(1);
    const bounds = await retry.boundingBox();
    const snapshot = await page.locator('body').ariaSnapshot();
    await page.screenshot({ path: path.join(process.argv[2], 'checkout.png'), animations: 'disabled' });
    process.stdout.write(JSON.stringify({ snapshot, bounds, assertionFailed, assertionPassed, orderCount: orders.length, scenario, observedAt: new Date().toISOString() }));
  } finally {
    await browser.close();
  }
})().catch(() => {
  // Do not retain browser errors, page text, paths or environment values.
  process.stderr.write('Synthetic fixture capture failed. Check npm dependencies and Chromium.\n');
  process.exitCode = 2;
});
