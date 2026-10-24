// Real local browser checks plus an explicitly synthetic UI-only provider fixture.
// This smoke never connects to TypeSafe or reads a provider credential.
const { chromium, expect } = require('@playwright/test');
const { execFileSync, spawn } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const { once } = require('node:events');

async function expectShopToFit(page) {
  // Check rendered geometry: an unscaled iframe clips both the card and button.
  await expect.poll(() => page.evaluate(() => {
    const stage = document.getElementById('stage').getBoundingClientRect();
    const shop = document.getElementById('shop').getBoundingClientRect();
    return Math.max(...['left', 'top', 'right', 'bottom'].map(edge => Math.abs(stage[edge] - shop[edge])));
  }), { message: 'The entire live shop must fit its preview, including after resize' }).toBeLessThan(1);
  const stage = await page.locator('#stage').boundingBox();
  const button = await page.frameLocator('#shop').getByRole('button').boundingBox();
  assert.ok(button && stage && button.x >= stage.x && button.y >= stage.y &&
    button.x + button.width <= stage.x + stage.width &&
    button.y + button.height <= stage.y + stage.height, 'Shop action must remain fully visible');
}

(async () => {
  fs.mkdirSync('.context', { recursive: true });
  const output = fs.mkdtempSync(path.resolve('.context/jev-interactive-smoke-'));
  const childEnv = {};
  for (const key of ['PATH', 'HOME', 'TMPDIR', 'TEMP', 'TMP', 'SYSTEMROOT', 'WINDIR', 'LOCALAPPDATA', 'PLAYWRIGHT_BROWSERS_PATH', 'XDG_CACHE_HOME', 'GOCACHE', 'GOMODCACHE', 'GOPATH', 'GOROOT', 'GOTOOLCHAIN']) {
    if (process.env[key] !== undefined) childEnv[key] = process.env[key];
  }
  const binary = path.join(output, process.platform === 'win32' ? 'jev-demo.exe' : 'jev-demo');
  execFileSync('go', ['build', '-o', binary, './demo/jev'], { env: childEnv, stdio: 'pipe' });
  const server = spawn(binary, ['--serve', '--port', '0'], { env: childEnv, stdio: ['ignore', 'pipe', 'pipe'] });
  const exited = once(server, 'exit');
  let browser;
  try {
    const origin = await new Promise((resolve, reject) => {
      const timeout = setTimeout(() => reject(new Error('Local demo did not start')), 30000);
      let log = '';
      server.stdout.on('data', chunk => {
        log += chunk;
        const match = log.match(/http:\/\/127\.0\.0\.1:\d+/);
        if (match) { clearTimeout(timeout); resolve(match[0]); }
      });
      server.once('error', () => { clearTimeout(timeout); reject(new Error('Local demo could not start')); });
      server.once('exit', () => { clearTimeout(timeout); reject(new Error('Local demo exited early')); });
    });
    browser = await chromium.launch({ headless: true, env: childEnv });
    const page = await browser.newPage({ viewport: { width: 1440, height: 1050 }, colorScheme: 'light' });
    const errors = [];
    page.on('pageerror', error => errors.push(error.name));
    let runs = 0;
    page.on('request', request => { if (request.url() === origin + '/api/run') runs++; });
    await page.goto(origin);
    await expect(page.getByRole('button', { name: 'Try the payment', exact: true })).toBeEnabled();
    await expectShopToFit(page);
    for (const width of [1920, 768, 640, 390, 1440]) {
      await page.setViewportSize({ width, height: 1050 });
      await expectShopToFit(page);
    }
    assert.equal(runs, 0, 'Opening the page must not start a check');
    await page.screenshot({ path: path.join(output, 'desktop-start.png'), fullPage: true });
    await page.getByRole('button', { name: 'Try the payment', exact: true }).click();
    await expect(page.frameLocator('#shop').getByRole('alert')).toHaveText('Payment declined. Try another card.');
    await expectShopToFit(page);
    await expect(page.frameLocator('#shop').getByRole('status')).toHaveCount(0);
    assert.deepEqual(await page.frameLocator('#shop').locator('body').evaluate(() => window.demoOrderStore.list()), []);
    const liveButton = await page.frameLocator('#shop').getByRole('button').boundingBox();
    await page.screenshot({ path: path.join(output, 'desktop-before-check.png'), fullPage: true });
    await page.getByRole('button', { name: 'Check with 9lives', exact: true }).click();
    await expect(page.locator('#check-title')).toHaveText('× No notebook order saved', { timeout: 30000 });
    await expect(page.locator('#metric-status')).toHaveText('No AI request');
    await expect(page.locator('#inference')).toBeHidden();
    assert.equal(runs, 1);
    const record = JSON.parse(await page.locator('#record').textContent());
    assert.equal(record.status, 'disabled');
    assert.equal(record.requestCount, 0);
    await expect(page.locator('#capture')).toBeVisible();
    await page.waitForFunction(() => document.querySelector('#capture').complete && document.querySelector('#capture').naturalWidth === 850);
    const outline = await page.locator('#observed').boundingBox();
    for (const key of ['x', 'y', 'width', 'height']) {
      assert.ok(Math.abs(outline[key] - liveButton[key]) < 1, 'Captured outline must match the live button position: ' + key);
    }
    await page.screenshot({ path: path.join(output, 'desktop-failure.png'), fullPage: true });
    await page.locator('#compare').uncheck();
    await expect(page.locator('#observed')).toBeHidden();
    await page.locator('#compare').check();
    await expect(page.locator('#observed')).toBeVisible();
    const reportPage = await browser.newPage();
    await reportPage.goto(new URL(await page.locator('#report-link').getAttribute('href'), origin).href);
    await expect(reportPage.getByText('Expected one notebook order → found none. Assertion failed as intended.')).toBeVisible();
    await reportPage.close();
    await page.getByRole('button', { name: 'Try the working payment', exact: true }).click();
    await page.getByRole('button', { name: 'Try the payment', exact: true }).click();
    await page.getByRole('button', { name: 'Check with 9lives', exact: true }).click();
    await expect(page.locator('#check-title')).toHaveText('✓ Notebook order saved', { timeout: 30000 });
    await expect(page.locator('#metric-status')).toHaveText('No AI request');
    await expect(page.locator('#check-copy')).toContainText('Found 1.');
    assert.equal(runs, 2);
    await page.screenshot({ path: path.join(output, 'desktop-success.png'), fullPage: true });
    await page.getByRole('button', { name: 'Switch color theme' }).click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await page.getByRole('button', { name: 'Switch color theme' }).click();
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.getByRole('button', { name: 'Try the payment', exact: true })).toBeEnabled();
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'Mobile horizontal overflow');
    await page.getByRole('button', { name: 'Working', exact: true }).click();
    await page.getByRole('button', { name: 'Try the payment', exact: true }).click();
    await page.getByRole('button', { name: 'Check with 9lives', exact: true }).click();
    await expect(page.locator('#check-title')).toHaveText('✓ Notebook order saved', { timeout: 30000 });
    await page.screenshot({ path: path.join(output, 'mobile-success.png'), fullPage: true });

    // Exercise AI presentation with a UI fixture, never a fabricated live result.
    await page.route('**/api/config', route => route.fulfill({ json: { live: true, images: true, remaining: 6, runsRemaining: 20 } }));
    let liveClicks = 0;
    await page.route('**/api/run', route => {
      if (!route.request().postDataJSON().live) return route.continue();
      liveClicks++;
      return route.fulfill({ json: { scenario: 'declined', passed: false, orderCount: 0, declined: 0.91, confirmed: 0.07, observed: null, predicted: { X: 12, Y: 50, Width: 76, Height: 8 }, image: '/unused-ui-fixture.png', report: '/', record: { status: 'answered', requestCount: 1, durationMs: 123 } } });
    });
    await page.reload();
    await page.getByRole('button', { name: 'Try the payment', exact: true }).click();
    await page.getByRole('button', { name: 'Check with 9lives', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Ask Jev to look', exact: true })).toBeEnabled({ timeout: 30000 });
    assert.equal(liveClicks, 0, 'Browser check must not implicitly ask AI');
    await page.getByRole('button', { name: 'Ask Jev to look', exact: true }).click();
    await expect(page.locator('#declined')).toHaveText('91.0% yes');
    await expect(page.locator('#confirmed')).toHaveText('7.0% yes');
    await expect(page.locator('#predicted')).toBeVisible();
    await expect(page.locator('#check-title')).toHaveText('× No notebook order saved');
    assert.equal(liveClicks, 1);
    // A delayed UI-only request verifies that cancellation releases the controls.
    await page.unroute('**/api/run');
    let held;
    await page.route('**/api/run', route => { held = route; });
    await page.getByRole('button', { name: 'Start over', exact: true }).click();
    await page.getByRole('button', { name: 'Try the payment', exact: true }).click();
    await page.getByRole('button', { name: 'Check with 9lives', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Cancel this check', exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Working', exact: true })).toBeDisabled();
    await page.getByRole('button', { name: 'Cancel this check', exact: true }).click();
    await expect(page.locator('#error')).toHaveText('Check cancelled. You can try again.');
    await expect(page.getByRole('button', { name: 'Check with 9lives', exact: true })).toBeEnabled();
    await held?.abort().catch(() => {});
    assert.deepEqual(errors, [], 'Browser JavaScript errors');
    // The same real capture helper must judge the ledger, not page wording.
    // These are controlled synthetic mutations, never model accuracy claims.
    const fixture = fs.readFileSync('demo/jev/checkout.html', 'utf8');
    function captureVariant(html, name) {
      const dir = path.join(output, name);
      fs.mkdirSync(dir);
      return JSON.parse(execFileSync(process.execPath, ['demo/jev/capture.cjs', dir], {
        input: html, env: childEnv, stdio: ['pipe', 'pipe', 'pipe'], timeout: 20000
      }));
    }
    const working = fixture.replace('data-scenario="declined"', 'data-scenario="success"');
    const changedCopy = captureVariant(working.replace('Thank you! Your notebook is on its way.',
      'All done! We are packing your notebook.'), 'changed-copy');
    assert.equal(changedCopy.assertionPassed, true, 'Confirmation wording must not decide the order assertion');
    assert.equal(changedCopy.orderCount, 1);
    const misleadingCopy = captureVariant(fixture.replace('Payment declined. Try another card.',
      'Thank you! Your notebook is on its way.'), 'misleading-copy');
    assert.equal(misleadingCopy.assertionFailed, true, 'A thank-you message cannot pass without a saved order');
    assert.equal(misleadingCopy.orderCount, 0);
    for (const capture of [changedCopy, misleadingCopy]) {
      assert.doesNotMatch(capture.snapshot, /order placed|order not placed|demo-order-1|demoOrderStore|data-scenario/i,
        'The model observation must not include a status label, scenario or hidden ledger');
    }
    assert.throws(() => captureVariant(working.replace('totalCents: 1200', 'totalCents: 9999'),
      'wrong-total'), 'A visible confirmation cannot pass an order with the wrong total');
    console.log('PASS: independent order assertions survive changed/misleading messages and reject wrong totals; no status labels; live shop fit at five widths, stable capture/button alignment, real failed and successful checkout assertions, saved evidence, offline continuation, theme persistence, mobile layout, cancellation controls; synthetic AI presentation only. No provider calls.');
    console.log('Screenshots:', output);
  } finally {
    await browser?.close();
    server.kill('SIGTERM');
    await exited;
  }
})().catch(error => { console.error(error.stack); process.exitCode = 1; });
