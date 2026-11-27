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
    assert.equal(await page.locator('[data-level]').count(), 6);

    // The player's own game runs in the framed room under its own policy.
    const race = page.getByLabel('Race the clock');
    await race.check();
    await page.getByRole('button', { name: 'Let’s play' }).click();
    const room = page.frame({ url: /\/hunt\/rooms\/cats\.html$/ });
    const real = await room.evaluate(() => window.catGame.targets().filter(p => !p.decoy).map(p => p.id));
    await room.locator(`[data-target="${real[0]}"]`).click();
    await page.locator('#counter', { hasText: '1 / 9 cats found' }).waitFor();

    // A recorded healthy run: real frames, every check, and a portable receipt.
    await watch.click();
    assert.equal(await page.locator('#clock').textContent(), 'Your timer is paused');
    await page.locator('#run-title', { hasText: '✓ Every click checked' }).waitFor({ timeout: 60000 });
    assert.match(await page.locator('#run-context').textContent(), /^Recorded live Jev \+ 9lives · Normal round · /);
    assert.equal(await page.locator('#timeline li').count(), 20);
    const receipt = await page.request.get(await page.locator('#receipt').evaluate(a => a.href));
    assert.equal((await receipt.json()).outcome, 'passed');
    await page.screenshot({ path: resolve(out, 'hunt-site-passed.png') });
    await page.getByRole('button', { name: 'Back to my game' }).click();
    await page.locator('#counter', { hasText: '1 / 9 cats found' }).waitFor();
    assert.ok(Number.parseInt(await page.locator('#clock').textContent(), 10) > 65, 'The replay did not spend the player’s time');

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

    // Finishing a level unlocks the next one, and a reload remembers it.
    await page.getByLabel('Plant a bug: one find earns no point').uncheck();
    await race.uncheck();
    await page.getByRole('button', { name: 'Let’s play' }).click();
    const cats = page.frame({ url: /\/hunt\/rooms\/cats\.html$/ });
    for (const id of await cats.evaluate(() => window.catGame.targets().filter(p => !p.decoy).map(p => p.id))) await cats.locator(`[data-target="${id}"]`).click();
    await page.locator('#card-title', { hasText: 'You found them all!' }).waitFor();
    await page.reload({ waitUntil: 'networkidle' });
    assert.ok(await page.getByRole('button', { name: /Dogs/ }).isEnabled(), 'Unlocked level remembered');
    assert.ok(await page.getByRole('button', { name: /Mice/ }).isDisabled(), 'Only finished levels unlock');

    // Reach both timed levels by playing the existing boards.
    const start = page.locator('#start');
    const playRound = async () => {
      await start.click();
      const board = page.frameLocator('#scene');
      const targets = await board.locator('body').evaluate(() => window.catGame.targets());
      return { targets, spot: id => board.locator(`[data-target="${id}"]`).click() };
    };
    const findAll = async ({ targets, spot }) => {
      for (const target of targets.filter(p => !p.decoy)) await spot(target.id);
    };
    const next = () => page.getByRole('button', { name: 'Next level →' }).click();
    for (const [name, seconds] of [['Dogs', 60], ['Mice', 50], ['Striped stranger', 40]]) {
      await page.getByRole('button', { name: new RegExp(name) }).click();
      assert.equal(await page.locator('#time-limit').textContent(), `· ${seconds}s`);
      await findAll(await playRound());
      await page.getByRole('button', { name: 'Next level →' }).waitFor();
    }
    await next();
    assert.equal(await page.locator('#picture-note').textContent(), 'Your game · Level 5 of 6');
    assert.ok(await race.isHidden(), 'The Copycats timer is mandatory');
    assert.ok(await watch.isHidden(), 'No recorded run is offered for this challenge');
    const pantry = page.getByRole('button', { name: /Pantry sprint/ });
    const clock = page.locator('#clock');
    const feedback = page.locator('#feedback');
    const startTime = new Date();
    await page.clock.install({ time: startTime });
    await page.clock.pauseAt(new Date(startTime.getTime() + 1000));
    let round = await playRound();
    assert.match(await clock.textContent(), /^30s left$/);
    await round.spot(round.targets.find(p => p.decoy).id);
    await feedback.getByText('That’s a toy cat!', { exact: false }).waitFor();
    assert.equal(await page.locator('#counter').textContent(), '0 / 6 cats found');
    await page.clock.fastForward(30_000);
    await feedback.getByText('Time’s up!', { exact: false }).waitFor();
    assert.equal(await clock.textContent(), '0s left');
    assert.ok(await pantry.isDisabled(), 'Timeout did not unlock the next level');
    await start.click(); // Retry returns to a fresh lobby.
    await playRound();
    await page.getByRole('button', { name: 'Show hiding places' }).click();
    assert.ok(await pantry.isDisabled(), 'Practice did not unlock the next level');
    await page.getByRole('button', { name: 'Restart level' }).click();
    await findAll(await playRound());
    await next();

    round = await playRound();
    assert.match(await clock.textContent(), /^20s left$/);
    const cheese = round.targets.find(p => p.decoy);
    const mouse = round.targets.find(p => !p.decoy);
    await round.spot(cheese.id);
    await feedback.getByText('−3s.', { exact: false }).waitFor();
    assert.equal(await clock.textContent(), '17s left');
    await round.spot(mouse.id);
    await round.spot(mouse.id);
    await feedback.getByText('You already found this one.', { exact: false }).waitFor();
    assert.equal(await clock.textContent(), '17s left', 'A repeat click did not subtract time');
    await page.clock.fastForward(15_000);
    await page.frameLocator('#scene').locator('#room').click({ position: { x: 4, y: 4 } });
    await feedback.getByText('Time’s up!', { exact: false }).waitFor();
    assert.equal(await page.locator('#counter').textContent(), '1 / 6 mice found');
    await start.click();
    await findAll(await playRound());
    await page.getByRole('heading', { name: 'Master detective!' }).waitFor();
    await page.getByRole('button', { name: 'Play from the beginning' }).click();
    assert.equal(await page.locator('#picture-note').textContent(), 'Your game · Level 1 of 6');
    await page.reload({ waitUntil: 'networkidle' });
    assert.ok(await pantry.isEnabled(), 'The sixth level stayed unlocked after reload');

    // All eight published recordings still load.
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
    // Private browsing or blocked storage still plays from level 1.
    const blocked = await browser.newContext();
    await blocked.addInitScript(() => Object.defineProperty(window, 'localStorage', { get() { throw new DOMException('Blocked', 'SecurityError'); } }));
    const privatePage = await blocked.newPage();
    const privateErrors = [];
    privatePage.on('pageerror', error => privateErrors.push(error.message));
    await privatePage.goto(origin + '/hunt.html', { waitUntil: 'networkidle' });
    await privatePage.getByRole('button', { name: 'Let’s play' }).waitFor();
    assert.ok(await privatePage.getByRole('button', { name: /Dogs/ }).isDisabled());
    assert.deepEqual(privateErrors, []);
    await blocked.close();
    console.log('PASS: website hunt under production headers: six playable levels, timer pause, expiry, penalties, retry, practice and remembered unlocks, healthy and planted-bug replays, portable receipt, stop without result, all eight recordings, five widths and blocked storage.');
  } finally { await browser.close(); await new Promise(done => server.close(done)); }
})().catch(error => { console.error(error); process.exitCode = 1; });
