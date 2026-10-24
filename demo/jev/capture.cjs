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
    await page.getByRole('button', { name: 'Place order', exact: true }).click();
    await expect(page.getByRole('alert')).toHaveText('Payment declined. Try another card.');
    await expect(page.getByRole('status')).toHaveText('Order not placed');
    let assertionFailed = false;
    try {
      await expect(page.getByRole('status')).toHaveText('Order placed', { timeout: 400 });
    } catch (error) {
      if (!error.matcherResult) throw error;
      assertionFailed = true;
    }
    if (!assertionFailed) throw new Error('The intended business assertion did not fail');
    const retry = page.getByRole('button', { name: 'Try again', exact: true });
    await expect(retry).toHaveCount(1);
    const bounds = await retry.boundingBox();
    const snapshot = await page.locator('body').ariaSnapshot();
    await page.screenshot({ path: path.join(process.argv[2], 'checkout.png'), animations: 'disabled' });
    process.stdout.write(JSON.stringify({ snapshot, bounds, assertionFailed, observedAt: new Date().toISOString() }));
  } finally {
    await browser.close();
  }
})().catch(() => {
  // Do not retain browser errors, page text, paths or environment values.
  process.stderr.write('Synthetic fixture capture failed. Check npm dependencies and Chromium.\n');
  process.exitCode = 2;
});
