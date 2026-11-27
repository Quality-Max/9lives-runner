// Records docs/assets/hunt.gif, the README's game preview, from the website's
// own hunt page: a recorded healthy round where every click is checked, then
// a round with the planted bug where the test fails. It plays the committed
// recordings in website/public/hunt and makes no AI request.
//
//   node demo/readme/record-hunt.cjs
//
// Run from the repository root after `npm ci`, with Chromium installed
// (`npm exec playwright install chromium`) and ffmpeg on PATH.
const { chromium } = require('playwright');
const { spawnSync } = require('node:child_process');
const { createServer } = require('node:http');
const { existsSync, mkdtempSync, readFileSync, readdirSync, rmSync, statSync } = require('node:fs');
const { tmpdir } = require('node:os');
const { extname, join, resolve } = require('node:path');

const root = resolve(__dirname, '../..');
const site = resolve(root, 'website/public');
const output = resolve(root, 'docs/assets/hunt.gif');
const types = { '.html': 'text/html; charset=utf-8', '.json': 'application/json', '.png': 'image/png', '.svg': 'image/svg+xml', '.woff2': 'font/woff2', '.js': 'text/javascript' };
const viewport = { width: 1200, height: 820 };

(async () => {
  const server = createServer((req, res) => {
    const file = resolve(site, '.' + decodeURIComponent(new URL(req.url, 'http://localhost').pathname));
    if (!file.startsWith(site + '/') || !existsSync(file) || !statSync(file).isFile()) return res.writeHead(404).end();
    res.writeHead(200, { 'content-type': types[extname(file)] || 'application/octet-stream' }).end(readFileSync(file));
  });
  await new Promise(done => server.listen(0, '127.0.0.1', done));
  const videos = mkdtempSync(join(tmpdir(), '9lives-hunt-video-'));
  const browser = await chromium.launch();
  let start = 0;
  try {
    const context = await browser.newContext({ viewport, recordVideo: { dir: videos, size: viewport } });
    const opened = Date.now();
    const page = await context.newPage();
    await page.goto(`http://127.0.0.1:${server.address().port}/hunt.html`, { waitUntil: 'networkidle' });
    const watch = page.getByRole('button', { name: 'Watch 9lives play' });
    await watch.waitFor();
    // The clip starts when 9lives starts playing; the page scrolls to the game.
    start = (Date.now() - opened) / 1000;
    await watch.click();
    await page.locator('#run-title', { hasText: '✓ Every click checked' }).waitFor({ timeout: 60000 });
    await page.waitForTimeout(2500);
    await page.getByLabel('Plant a bug: one find earns no point').check();
    await watch.click();
    await page.locator('#run-title', { hasText: '× The test failed' }).waitFor({ timeout: 60000 });
    await page.waitForTimeout(3500);
    await context.close();
  } finally {
    await browser.close();
    server.close();
  }
  const [video] = readdirSync(videos).filter(name => name.endsWith('.webm'));
  if (!video) throw new Error('no video was recorded');
  // 1.5x speed, 10 frames per second, 820 pixels wide, one shared palette.
  const filters = 'setpts=PTS/1.5,fps=10,scale=820:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=64:stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle';
  const ffmpeg = spawnSync('ffmpeg', ['-y', '-v', 'error', '-ss', start.toFixed(2), '-i', join(videos, video), '-filter_complex', filters, '-loop', '0', output], { stdio: 'inherit' });
  rmSync(videos, { recursive: true, force: true });
  if (ffmpeg.status !== 0) throw new Error('ffmpeg failed');
  console.log(`Wrote ${output} (${Math.round(statSync(output).size / 1024)} KB)`);
})().catch(error => { console.error(error.message); process.exitCode = 1; });
