// Ordinary Playwright assertions, scheduled and receipted by the Go 9lives runner.
import { test, expect } from '@playwright/test';
import fs from 'node:fs/promises';
import path from 'node:path';

// @9l-requirement cat-hunt
test('Find every requested figure. Check every click.', async ({ page }) => {
  const dir = process.env.CAT_HUNT_DIR;
  const input = JSON.parse(await fs.readFile(path.join(dir, 'input.json'), 'utf8'));
  let frame = 0, checked = 0, phase = 'setup', target = null;
  const events = [];
  const score = () => page.locator('.found').count();
  async function progress(stage, message) {
    const image = `frame-${frame++}.png`;
    await page.screenshot({ path: path.join(dir, image), animations: 'disabled' });
    events.push({ stage, message, found: await score(), checked });
    const value = { stage, message, found: await score(), checked, image, target, events };
    await fs.writeFile(path.join(dir, 'progress.tmp'), JSON.stringify(value));
    await fs.rename(path.join(dir, 'progress.tmp'), path.join(dir, 'progress.json'));
  }
  await page.route('**/*', route => route.abort());
  try {
    await page.setContent(await fs.readFile(path.join(dir, 'room.html'), 'utf8'));
    await expect(page.locator('#animals > [data-target]')).toHaveCount(9);
    await expect(page.locator('.found')).toHaveCount(0);
    // Read only actionable controls and their geometry for the provider. No answer map.
    const candidates = [];
    for (let id = 0; id < 9; id++) {
      const control = page.locator(`[data-target="${id}"]`);
      await expect(control).toBeVisible();
      const b = await control.boundingBox();
      candidates.push({ id, box: { X: b.x/8.5, Y: b.y/6, Width: b.width/8.5, Height: b.height/6 } });
    }
    phase = 'selection';
    await progress('looking', input.live ? 'Jev is choosing matching figures from the picture…' : 'Using the game’s known hiding places. No AI request.');
    let selected;
    if (input.live) {
      // Number only observed geometry; no answer-map values enter this image.
      await page.evaluate(spots => {
        const overlay = document.createElement('div'); overlay.id = 'jev-regions';
        Object.assign(overlay.style, { position: 'fixed', inset: '0', pointerEvents: 'none', zIndex: '9999' });
        for (const { id, box: b } of spots) {
          const region = document.createElement('div');
          Object.assign(region.style, { position: 'absolute', left: `${b.X}%`, top: `${b.Y}%`, width: `${b.Width}%`, height: `${b.Height}%`, outline: '2px solid #1455c0' });
          const label = document.createElement('span'); label.textContent = String(id + 1);
          Object.assign(label.style, { position: 'absolute', left: '0', bottom: '100%', background: '#1455c0', color: 'white', font: 'bold 12px sans-serif', padding: '1px 3px' });
          region.append(label); overlay.append(region);
        }
        document.body.append(overlay);
      }, candidates);
      let png;
      try { png = (await page.screenshot()).toString('base64'); }
      finally { await page.locator('#jev-regions').evaluate(el => el.remove()); }
      // Private attempt-owned Go bridge. Credentials never enter this worker.
      const response = await fetch(process.env.CAT_HUNT_PROVIDER, { method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Hunt-Token': process.env.CAT_HUNT_TOKEN },
        body: JSON.stringify({ candidates, png }),
        signal: AbortSignal.timeout(25000) });
      expect(response.ok, 'Jev must return a usable decision before any click').toBe(true);
      const decision = await response.json();
      expect(decision.resolved, 'Jev must be confident about all nine spots; uncertainty stops the run').toBe(true);
      selected = decision.ids;
    } else {
      selected = await page.evaluate(() => window.catGame.targets().filter(p => !p.decoy).map(p => p.id));
    }
    expect(Array.isArray(selected)).toBe(true);
    expect(new Set(selected).size).toBe(selected.length);
    expect(selected.every(id => Number.isInteger(id) && id >= 0 && id < 9)).toBe(true);
    // The independent oracle is consulted only AFTER selection. It is never sent to Jev.
    const truth = await page.evaluate(() => window.catGame.targets());
    const real = truth.filter(p => !p.decoy).map(p => p.id);
    phase = 'selection-check';
    // @9l-outcome matching-spots
    expect([...selected].sort(), 'Selected spots must include every requested figure and no lookalike').toEqual([...real].sort());
    for (const id of selected) {
      phase = 'cat-click'; target = candidates[id].box;
      await progress('clicking', `Clicking hiding spot ${id+1}…`);
      const control = page.locator(`[data-target="${id}"]`);
      await expect(control).toBeVisible(); await expect(control).toBeEnabled();
      const b = await control.boundingBox(), old = candidates[id].box;
      // Reject changed geometry rather than acting on an old image.
      expect([b.x/8.5,b.y/6,b.width/8.5,b.height/6]).toEqual([old.X,old.Y,old.Width,old.Height]);
      const before = await score();
      await control.click(); // Mutations have no Playwright or runner retry.
      await test.step(`Figure at spot ${id+1}: one click must add exactly one to the score`, async () => {
        // @9l-outcome score-increase
        await expect(page.locator('.found')).toHaveCount(before+1);
      });
      checked++;
      await progress('checked', `Figure ${checked}: score ${before} → ${before+1}. Checked!`);
      await page.waitForTimeout(450); // Presentation pacing only, never a retry.
    }
    phase = 'repeat-check'; target = null;
    await page.locator(`[data-target="${selected[0]}"]`).click();
    // @9l-outcome repeat-safe
    await expect(page.locator('.found'), 'Repeated clicks must not count a figure twice').toHaveCount(real.length);
    phase = 'cushion-check';
    for (const cushion of truth.filter(p => p.decoy)) {
      await page.locator(`[data-target="${cushion.id}"]`).click();
      // @9l-outcome decoy-safe
      await expect(page.locator('.found'), 'Lookalikes must never change the score').toHaveCount(real.length);
    }
    phase = 'complete';
    // @9l-outcome all-found
    await expect(page.locator('.found'), 'Every matching figure must be found').toHaveCount(real.length);
    await progress('passed', `All ${real.length} figures checked. ${truth.length-real.length} lookalikes left alone.`);
  } catch (error) {
    const message = { setup: 'The browser could not prepare the room.', selection: 'Jev could not choose every spot confidently. No clicks were made.',
      'selection-check': 'The proposed spots did not match all the requested figures. No clicks were made.',
      'cat-click': 'Caught it! A correct click did not add one to the score.', 'repeat-check': 'A repeat click changed the score.',
      'cushion-check': 'A lookalike changed the score.', complete: 'The final score did not match every matching figure.' }[phase];
    await progress(phase === 'selection' ? 'selection-stopped' : phase === 'selection-check' ? 'selection-rejected' : 'failed', message).catch(() => {});
    throw error; // Keep the original assertion for the runner’s real receipt.
  }
});
