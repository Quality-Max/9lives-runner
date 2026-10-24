// The answer map is used only for browser assertions; Jev receives a clean PNG.
const { chromium, expect } = require('@playwright/test');
const path = require('node:path');
(async () => {
  const chunks = [];
  for await (const chunk of process.stdin) chunks.push(chunk);
  const html = Buffer.concat(chunks).toString('utf8');
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 850, height: 600 }, deviceScaleFactor: 1 });
    await page.route('**/*', route => route.abort());
    await page.setContent(html);
    const scenario = await page.locator('body').getAttribute('data-scenario');
    if (!['cats', 'cat-decoys'].includes(scenario)) throw Error('Unknown room');
    await expect(page.locator('#animals > [data-target]')).toHaveCount(9);
    for (const animal of await page.locator('#animals > [data-target]').all()) await expect(animal).toBeVisible();
    const targets = await page.evaluate(() => window.catGame.targets());
    const cats = targets.filter(p => !p.decoy), decoys = targets.filter(p => p.decoy);
    expect(cats).toHaveLength(scenario === 'cats' ? 9 : 6);
    expect(decoys).toHaveLength(scenario === 'cats' ? 0 : 3);
    await expect(page.locator('.found')).toHaveCount(0);
    // Save the unmarked scene before exercising its targets.
    await page.screenshot({ path: path.join(process.argv[2], 'checkout.png'), animations: 'disabled' });
    const observedAt = new Date().toISOString();
    const tap = p => page.mouse.click((p.box.X+p.box.Width/2)*8.5, (p.box.Y+p.box.Height/2)*6);
    await page.locator(`[data-target="${cats[0].id}"]`).press('Enter');
    await expect(page.locator('.found')).toHaveCount(1);
    for (const [i, cat] of cats.entries()) {
      await tap(cat);
      await expect(page.locator('.found')).toHaveCount(i+1);
    }
    await page.locator(`[data-target="${cats[0].id}"]`).press('Enter');
    await tap(cats[0]);
    for (const decoy of decoys) {
      await page.locator(`[data-target="${decoy.id}"]`).press(' ');
      await tap(decoy);
    }
    await expect(page.locator('.found')).toHaveCount(cats.length);
    expect(await page.evaluate(() => window.catGame.reveal())).toBe(cats.length);
    process.stdout.write(JSON.stringify({ snapshot: 'A clean illustrated room. Judge only the screenshot.', bounds: { X:0,Y:0,Width:850,Height:600 }, catBoxes: cats.map(p=>p.box), assertionFailed:false, assertionPassed:true, scenario, observedAt }));
  } finally { await browser.close(); }
})().catch(() => { process.stderr.write('Synthetic cat scene check failed.\n'); process.exitCode=2; });
