// Browser qualification of the website hunt: static files served with the
// production headers from website/vercel.json, played in real Chromium.
const assert = require('node:assert/strict');
const { chromium } = require('playwright');
const { createServer } = require('node:http');
const { existsSync, mkdirSync, readFileSync, statSync } = require('node:fs');
const { extname, resolve } = require('node:path');
const root = resolve(__dirname, '..');
const site = resolve(root, 'website/public');
const config = require('../website/vercel.json');
const types = { '.html': 'text/html; charset=utf-8', '.json': 'application/json', '.png': 'image/png', '.svg': 'image/svg+xml', '.woff2': 'font/woff2', '.js': 'text/javascript', '.ico': 'image/x-icon' };

(async () => {
  const out = resolve(root, '.context');
  mkdirSync(out, { recursive: true });
  const server = createServer((req, res) => {
    const path = decodeURIComponent(new URL(req.url, 'http://localhost').pathname);
    const file = resolve(site, '.' + path);
    if (!file.startsWith(site + '/') || !existsSync(file) || !statSync(file).isFile()) return res.writeHead(404).end();
    const headers = { 'content-type': types[extname(file)] || 'application/octet-stream' };
    for (const rule of config.headers) if (new RegExp(`^${rule.source}$`).test(path)) for (const h of rule.headers) headers[h.key.toLowerCase()] = h.value;
    res.writeHead(200, headers).end(readFileSync(file));
  });
  await new Promise(done => server.listen(0, '127.0.0.1', done));
  const origin = `http://127.0.0.1:${server.address().port}`;
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 1366, height: 1000 } });
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    page.on('console', message => { if (message.type() === 'error') errors.push(message.text()); });
    page.on('response', response => { if (response.status() >= 400) errors.push(`${response.status()} ${response.url()}`); });
    await page.goto(origin + '/hunt.html', { waitUntil: 'networkidle' });
    assert.equal(await page.locator('#engine-mode').textContent(), 'Recorded run');
    const watch = page.getByRole('button', { name: 'Watch 9lives play' });
    await watch.waitFor();
    assert.ok(await watch.isEnabled(), 'Watch is ready without a server');

    // The player's own game runs in the framed room under its own policy.
    await page.getByRole('button', { name: 'Let’s play' }).click();
    const room = page.frame({ url: /\/hunt\/rooms\/cats\.html$/ });
    const real = await room.evaluate(() => window.catGame.targets().filter(p => !p.decoy).map(p => p.id));
    await room.locator(`[data-target="${real[0]}"]`).click();
    await page.locator('#counter', { hasText: '1 / 9 cats found' }).waitFor();

    // A recorded healthy run: real frames, every check, and a portable receipt.
    await watch.click();
    await page.locator('#run-title', { hasText: '✓ Every click checked' }).waitFor({ timeout: 60000 });
    assert.match(await page.locator('#run-context').textContent(), /^Recorded live Jev \+ 9lives · Normal round · /);
    assert.equal(await page.locator('#timeline li').count(), 20);
    const receipt = await page.request.get(await page.locator('#receipt').evaluate(a => a.href));
    assert.equal((await receipt.json()).outcome, 'passed');
    await page.screenshot({ path: resolve(out, 'hunt-site-passed.png') });
    await page.getByRole('button', { name: 'Back to my game' }).click();
    await page.locator('#counter', { hasText: '1 / 9 cats found' }).waitFor();

    // The planted bug restarts the level and the recorded run catches it.
    await page.getByLabel('Plant a bug: one find earns no point').check();
    await watch.click();
    await page.locator('#run-title', { hasText: '× The test failed' }).waitFor({ timeout: 30000 });
    assert.match(await page.locator('#feedback').textContent(), /did not add one to the score/);
    await page.screenshot({ path: resolve(out, 'hunt-site-failed.png') });
    await page.getByRole('button', { name: 'Back to my game' }).click();

    // Stopping a replay never shows a result.
    await watch.click();
    await page.getByRole('button', { name: 'Stop this run' }).click();
    await page.locator('#error', { hasText: 'Stopped. No passing result.' }).waitFor();
    assert.equal(await page.locator('#run-result').isHidden(), true);

    // Every level's recordings load.
    for (const scenario of ['cats', 'dogs', 'mice', 'striped']) for (const variant of ['healthy', 'broken']) {
      const base = `${origin}/hunt/replays/${scenario}-${variant}/`;
      const run = await (await page.request.get(base + 'replay.json')).json();
      for (const step of run.steps) assert.equal((await page.request.get(base + step.image)).status(), 200);
    }

    for (const width of [320, 390, 768, 1366, 1920]) {
      await page.setViewportSize({ width, height: 900 });
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `overflow at ${width}`);
    }
    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({ path: resolve(out, 'hunt-site-mobile.png'), fullPage: true });
    assert.deepEqual(errors, []);
    console.log('PASS: website hunt under production headers: framed manual play, recorded healthy and planted-bug runs, portable receipt, stop without result, all eight recordings, five widths, no errors.');
  } finally { await browser.close(); await new Promise(done => server.close(done)); }
})().catch(error => { console.error(error); process.exitCode = 1; });
