// Exercise the shipped file with the same CSP as 9lives.run.
const assert = require('node:assert/strict');
const { readFileSync, mkdirSync } = require('node:fs');
const { resolve } = require('node:path');
const { createServer } = require('node:http');
const { chromium } = require('playwright');

(async () => {
  const root = resolve(__dirname, '..');
  mkdirSync(resolve(root, '.context'), { recursive: true });
  const canonical = readFileSync(resolve(root, 'docs/explainer/demo.html'));
  assert.deepEqual(readFileSync(resolve(root, 'website/public/demo.html')), canonical, 'Run node scripts/sync-demo.cjs');
  const policy = require('../website/vercel.json').headers.find(rule => rule.source === '/demo.html').headers.find(h => h.key === 'Content-Security-Policy').value;
  const server = createServer((req, res) => {
    if (req.url === '/favicon.ico') return res.writeHead(204).end();
    res.writeHead(200, { 'content-type': 'text/html; charset=utf-8', 'content-security-policy': policy });
    res.end(canonical);
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  let browser;
  try {
    browser = await chromium.launch();
    const url = process.argv[2] || `http://127.0.0.1:${server.address().port}/demo.html`;
    const context = await browser.newContext({ colorScheme: 'light' });
    const page = await context.newPage();
    let errors = 0, externalRequests = 0;
    page.on('pageerror', () => errors++);
    page.on('console', message => { if (message.type() === 'error') errors++; });
    page.on('request', request => {
      if (/^https?:/.test(request.url()) && new URL(request.url()).origin !== new URL(url).origin) externalRequests++;
    });
    await page.goto(url);
    const theme = () => page.locator('html').getAttribute('data-theme');
    assert.equal(await theme(), 'light');
    await page.emulateMedia({ colorScheme: 'dark' });
    await page.waitForFunction(() => document.documentElement.dataset.theme === 'dark');
    await page.getByRole('button', { name: 'Dark theme', exact: true }).click();
    assert.equal(await theme(), 'light');
    await page.reload();
    assert.equal(await theme(), 'light', 'Explicit choice survives reload and overrides system');
    for (const selected of ['light', 'dark']) {
      if (await theme() !== selected) await page.locator('#theme-toggle').click();
      assert.equal(await page.locator('.brand-home img:visible').count(), 1);
      assert.ok(await page.locator('.brand-home img:visible').evaluate(img => img.complete && img.naturalWidth > 0));
      await page.evaluate(() => document.fonts.ready);
      assert.ok(await page.evaluate(() => document.fonts.check('16px "DM Sans"') && document.fonts.check('16px "Space Grotesk"')));
      assert.equal(await page.locator('body').evaluate(el => getComputedStyle(el).backgroundColor), selected === 'dark' ? 'rgb(9, 9, 11)' : 'rgb(250, 250, 250)');
      for (const width of [375, 768, 1366]) {
        await page.setViewportSize({ width, height: 1000 });
        for (const tab of ['locator', 'coverage', 'flow', 'live', 'proof']) {
          await page.locator('#journey-' + tab).click();
          assert.equal(await page.locator('#journey-' + tab).getAttribute('aria-selected'), 'true');
          assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${selected} ${tab} at ${width}`);
        }
      }
      await page.locator('[data-proof-step="2"]').click();
      assert.equal(await page.locator('#proof-verdict').innerText(), 'confirmed');
      await page.selectOption('#proof-log-filter', 'all');
      assert.ok(await page.locator('#proof-lines').evaluate(el => el.scrollHeight > el.clientHeight));
      await page.locator('#proof-log-end').click();
      assert.ok(await page.locator('#proof-lines').evaluate(el => el.scrollTop > 0));
      if (!process.argv[2]) await page.screenshot({ path: resolve(root, `.context/demo-${selected}.png`), fullPage: true });
    }
    assert.equal(errors, 0, 'No script, request or CSP errors');
    assert.equal(externalRequests, 0, 'Demo loads without external resources');
    const blocked = await browser.newContext({ colorScheme: 'dark' });
    await blocked.addInitScript(() => {
      Object.defineProperty(window, 'localStorage', { get() { throw new DOMException('Disabled', 'SecurityError'); } });
    });
    const privatePage = await blocked.newPage();
    await privatePage.goto(url);
    await privatePage.locator('#theme-toggle').click();
    assert.equal(await privatePage.locator('html').getAttribute('data-theme'), 'light');
    console.log('PASS: light/dark, system preference, persisted choice, blocked storage, official fonts/logos under production CSP, 30 responsive tab views, confirmed evidence and scrollable logs.');
  } finally {
    if (browser) await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
