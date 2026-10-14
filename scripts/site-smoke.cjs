// Browser qualification of the marketing pages and the narrow embedding contract.
const assert = require('node:assert/strict');
const { chromium } = require('playwright');
const { createServer } = require('node:http');
const { readFileSync, existsSync, mkdirSync } = require('node:fs');
const { resolve, extname } = require('node:path');
const root = resolve(__dirname, '..');
const config = require('../website/vercel.json');
function policy(path) {
  const matches = config.headers.filter(r => new RegExp(`^${r.source}$`).test(path)).flatMap(r => r.headers).filter(h => h.key === 'Content-Security-Policy');
  assert.equal(matches.length, 1);
  return matches[0].value;
}
(async () => {
  mkdirSync(resolve(root, '.context'), { recursive: true });
  const server = createServer((req, res) => {
    const path = decodeURIComponent(new URL(req.url, 'http://localhost').pathname);
    const file = resolve(root, 'docs/explainer', '.' + (path === '/' ? '/index.html' : path));
    if (!file.startsWith(resolve(root, 'docs/explainer') + '/') || !existsSync(file)) return res.writeHead(404).end();
    const type = { '.html': 'text/html', '.css': 'text/css', '.svg': 'image/svg+xml', '.woff2': 'font/woff2' }[extname(file)];
    res.writeHead(200, { 'content-type': type || 'application/octet-stream' });
    res.end(readFileSync(file));
  });
  await new Promise(done => server.listen(0, '127.0.0.1', done));
  const browser = await chromium.launch();
  try {
    for (const [name, url] of [['home', process.argv[2] || 'http://localhost:3000/'], ['overview', `http://127.0.0.1:${server.address().port}/`]]) {
      const context = await browser.newContext({ colorScheme: 'light', permissions: ['clipboard-read', 'clipboard-write'] });
      const page = await context.newPage();
      let errors = 0;
      page.on('pageerror', () => errors++);
      page.on('console', message => { if (message.type() === 'error') errors++; });
      page.on('response', response => { if (response.status() >= 400) errors++; });
      await page.goto(url, { waitUntil: 'networkidle' });
      const theme = () => page.locator('html').getAttribute('data-theme');
      assert.equal(await theme(), 'light');
      await page.emulateMedia({ colorScheme: 'dark' });
      await page.waitForFunction(() => document.documentElement.dataset.theme === 'dark');
      await page.getByRole('button', { name: 'Dark theme', exact: true }).click();
      assert.equal(await theme(), 'light');
      await page.reload({ waitUntil: 'networkidle' });
      assert.equal(await theme(), 'light');
      assert.equal(await page.getByRole('button', { name: 'Dark theme', exact: true }).getAttribute('aria-pressed'), 'false');
      for (const selected of ['light', 'dark']) {
        if (await theme() !== selected) await page.getByRole('button', { name: 'Dark theme', exact: true }).click();
        for (const width of [320, 390, 768, 1366, 1920]) {
          await page.setViewportSize({ width, height: 1000 });
          assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${name} ${selected} overflow at ${width}`);
          await page.evaluate(() => document.fonts.ready);
          assert.ok(await page.evaluate(() => document.fonts.check('16px "DM Sans"') && document.fonts.check('16px "Space Grotesk"')));
          assert.equal(await page.locator('.qualitymax-brand img:visible').count(), 2);
          assert.ok(await page.locator('img:visible').evaluateAll(async imgs => { await Promise.all(imgs.map(img => img.decode())); return imgs.every(img => img.complete && img.naturalWidth > 0); }));
        }
        await page.setViewportSize({ width: 1440, height: 1000 });
        await page.screenshot({ path: resolve(root, `.context/9lives-${name}-${selected}.png`) });
        await page.setViewportSize({ width: 390, height: 844 });
        await page.screenshot({ path: resolve(root, `.context/9lives-${name}-${selected}-mobile.png`), fullPage: true });
      }
      await page.getByRole('link', { name: /run it yourself/i }).click();
      assert.equal(new URL(page.url()).hash, '#start');
      const command = 'curl -fsSL https://9lives.run/install.sh | sh';
      await page.getByRole('button', { name: `Copy ${command}`, exact: true }).click();
      await page.getByRole('button', { name: `Copy ${command}`, exact: true }).getByText('Copied', { exact: true }).waitFor();
      assert.equal(await page.evaluate(() => navigator.clipboard.readText()), command);
      for (const chapter of ['locator', 'coverage', 'flow', 'live', 'proof']) {
        const link = page.locator(`.story-card[href$="demo.html#${chapter}"]`);
        await link.click();
        assert.equal(await page.locator(`#journey-${chapter}`).getAttribute('aria-selected'), 'true', `deep link ${chapter}`);
        await page.goBack({ waitUntil: 'networkidle' });
      }
      assert.equal(errors, 0, `${name} errors`);
      await context.close();
      const blocked = await browser.newContext({ colorScheme: 'dark' });
      await blocked.addInitScript(() => Object.defineProperty(window, 'localStorage', { get() { throw new DOMException('Blocked', 'SecurityError'); } }));
      const privatePage = await blocked.newPage();
      await privatePage.goto(url, { waitUntil: 'networkidle' });
      await privatePage.getByRole('button', { name: 'Dark theme', exact: true }).click();
      await privatePage.emulateMedia({ colorScheme: 'light' });
      await privatePage.emulateMedia({ colorScheme: 'dark' });
      assert.equal(await privatePage.locator('html').getAttribute('data-theme'), 'light', 'Explicit blocked-storage choice wins');
      await blocked.close();
    }
    for (const parentOrigin of ['https://qualitymax.io', 'https://www.qualitymax.io', 'https://unrelated.example']) {
      const context = await browser.newContext();
      await context.route('**/*', async route => {
        const url = new URL(route.request().url());
        if (url.origin === parentOrigin) return route.fulfill({ contentType: 'text/html', headers: { 'content-security-policy': "default-src 'self'; frame-src https://9lives.run" }, body: '<iframe title="9lives" src="https://9lives.run/demo.html#proof"></iframe>' });
        if (url.origin === 'https://9lives.run') return route.fulfill({ contentType: 'text/html', headers: { 'content-security-policy': policy('/demo.html') }, body: readFileSync(resolve(root, 'website/public/demo.html'), 'utf8') });
        return route.abort();
      });
      const page = await context.newPage();
      await page.goto(parentOrigin);
      if (parentOrigin.includes('qualitymax.io')) {
        const tab = page.frameLocator('iframe').locator('#journey-proof');
        await tab.waitFor();
        assert.equal(await tab.getAttribute('aria-selected'), 'true');
      } else {
        await page.waitForTimeout(300);
        assert.equal(await page.frameLocator('iframe').locator('#journey-proof').count(), 0, 'Unrelated origins cannot embed');
      }
      await context.close();
    }
    const page = await browser.newPage({ viewport: { width: 1200, height: 630 } });
    await page.goto('file://' + resolve(root, 'website/public/og.svg'));
    await page.screenshot({ path: resolve(root, 'website/public/og.png') });
    console.log('PASS: 20 responsive views, both pages/themes/fonts/logos, theme persistence and blocked storage, clipboard, all five chapter deep links, two approved embed origins and one rejected origin.');
  } finally { await browser.close(); await new Promise(done => server.close(done)); }
})().catch(error => { console.error(error); process.exitCode = 1; });
