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

    // Real four-level game and ordinary Playwright runs owned by 9lives.
    await page.setViewportSize({ width: 1440, height: 1050 });
    const beforeCats = runs;
    let hunts = 0;
    page.on('request', request => { if(request.url() === origin+'/api/hunt') hunts++; });
    await page.goto(origin+'/cats');
    await expect(page.locator('#watch')).toBeEnabled();
    assert.equal(runs,beforeCats);
    assert.equal(hunts,0,'Opening the game must not run a test');
    await expect(page.locator('#use-jev')).toHaveCount(0);
    await expect(page.locator('#engine-mode')).toHaveText('Free demo');
    await expect(page.locator('#look-title')).toHaveText('Use the hiding-place map');
    await expect(page.locator('[data-level="1"]')).toBeDisabled();
    await expect(page.locator('#timed')).not.toBeChecked();
    for (const width of [1920,950,768,640,390,1440]) {
      await page.setViewportSize({width,height:1050});
      await expect.poll(() => page.evaluate(() => {
        const a=document.querySelector('#stage').getBoundingClientRect(),b=document.querySelector('#scene').getBoundingClientRect();
        return Math.max(...['left','top','right','bottom'].map(k=>Math.abs(a[k]-b[k])));
      })).toBeLessThan(1);
      assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'Game must fit the viewport');
    }
    const targets=()=>page.frameLocator('#scene').locator('body').evaluate(()=>window.catGame.targets());
    async function tapTarget(target){
      await page.locator('#stage').scrollIntoViewIfNeeded();
      const stage=await page.locator('#stage').boundingBox();
      await page.mouse.click(stage.x+(target.box.X+target.box.Width/2)*stage.width/100,stage.y+(target.box.Y+target.box.Height/2)*stage.height/100);
    }
    async function finishRun(broken,goal){
      await page.locator('#broken').setChecked(broken);
      await expect(page.locator('#run-result')).toBeHidden();
      await page.locator('#watch').click();
      await expect(page.locator('#run-result')).toBeVisible({timeout:80000});
      const href=await page.locator('#receipt').getAttribute('href');
      const summary=await(await page.request.get(origin+href)).json();
      assert.equal(summary.outcome,broken?'failed':'passed');
      assert.equal(summary.receipts.length,1);
      const receipt=summary.receipts[0];
      assert.equal(receipt.attempt,1);assert.equal(receipt.executedTests,1);assert.equal(receipt.validated,true);
      assert.equal(receipt.status,broken?'failed':'passed');
      assert.equal(receipt.assertionCoverage,'unknown','Ordinary execution must not invent independent assertion coverage');
      if(broken){
        assert.equal(receipt.failures[0].class,'assertion','A timeout or setup error is not proof of a broken score');
        assert.match(receipt.failures[0].message,/expect\(locator\)\.toHaveCount/);
        await expect(page.locator('#feedback')).toContainText('did not add one');
      }else{
        await expect(page.locator('#run-title')).toHaveText('✓ Every click checked');
        await expect(page.locator('#counter')).toHaveText(`9lives: ${goal} / ${goal} found`);
      }
      return summary;
    }
    await page.screenshot({path:path.join(output,'game-start.png'),fullPage:true});
    for(const [index,scenario,goal,noun] of [[0,'cats',9,'cats'],[1,'dogs',9,'dogs'],[2,'mice',6,'mice'],[3,'striped',1,'striped stranger']]){
      await expect(page.frameLocator('#scene').locator('body')).toHaveAttribute('data-scenario',scenario);
      await page.locator('#start').click();
      const spots=await targets(),real=spots.filter(p=>!p.decoy);
      assert.equal(real.length,goal);
      await expect(page.locator('#clock')).toHaveText('Take your time');
      if(index<3){
        await tapTarget(real[0]);
        await tapTarget(real[0]);
        await expect(page.locator('#counter')).toHaveText(`1 / ${goal} ${noun} found`);
        await expect(page.locator('#feedback')).toContainText('already found');
      }
      if(index===2){
        await tapTarget(spots.find(p=>p.decoy));
        await expect(page.locator('#feedback')).toContainText('cheese');
        await expect(page.locator('#counter')).toHaveText('1 / 6 mice found');
      }
      await page.screenshot({path:path.join(output,`level-${scenario}.png`),fullPage:true});
      await finishRun(false,goal);
      if(index===0){
        // Real runner events must fill the space beneath the picture, without a disclosure.
        await expect(page.getByRole('region',{name:'Clicks and checks'})).toBeVisible();
        await expect(page.locator('.scene-pane #timeline')).toContainText('score 8 → 9');
        await expect(page.locator('.robot #timeline')).toHaveCount(0);
        for(const width of [1920,950,768,640,390,1440]){
          await page.setViewportSize({width,height:1050});
          const log=await page.locator('#run-details').boundingBox();
          const tools=await page.locator('.scene-tools').boundingBox();
          const pane=await page.locator('.scene-pane').boundingBox();
          const guide=await page.locator('.guide').boundingBox();
          assert.ok(log.y>=tools.y+tools.height-1,'Activity must sit beneath the picture tools');
          assert.ok(log.x>=pane.x && log.x+log.width<=pane.x+pane.width,'Activity must fit the scene pane');
          if(width<=720)assert.ok(log.y+log.height<=guide.y+1,'Mobile activity must stay above the guide');
          assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'Activity must not overflow the viewport');
          if(width===390)await page.screenshot({path:path.join(output,'runner-cats-activity-mobile.png'),fullPage:true});
        }
      }
      await page.screenshot({path:path.join(output,`runner-${scenario}-pass.png`),fullPage:true});
      await page.locator('#back').click();
      await expect(page.locator('#counter')).toHaveText(`${index<3?1:0} / ${goal} ${noun} found`);
      await finishRun(true,goal);
      await page.screenshot({path:path.join(output,`runner-${scenario}-fail.png`),fullPage:true});
      await page.locator('#back').click();
      // Switching the bug on restarted the player's level too. Both rounds use the same defect.
      await expect(page.locator('#counter')).toHaveText(`0 / ${goal} ${noun} found`);
      await expect(page.locator('#picture-note')).toContainText('Bug planted');
      await page.locator('#start').click();
      for(const spot of real)await tapTarget(spot);
      await expect(page.locator('#counter')).toHaveText(`${goal-1} / ${goal} ${noun} found`);
      await expect(page.locator('#curtain')).toBeHidden();
      await tapTarget(real[1]||real[0]);
      await expect(page.locator('#feedback')).toContainText('planted bug');
      if(index===0)await page.screenshot({path:path.join(output,'player-cats-bug.png'),fullPage:true});
      await page.locator('#broken').uncheck();
      await expect(page.locator('#counter')).toHaveText(`0 / ${goal} ${noun} found`);
      await expect(page.locator('#run-result')).toBeHidden();
      await expect(page.locator('#picture-note')).not.toContainText('Bug planted');
      await page.locator('#start').click();
      for(const spot of real)await tapTarget(spot);
      await expect(page.locator('#card-title')).toHaveText(index===3?'Master detective!':'You found them all!');
      if(index<3){await expect(page.locator(`[data-level="${index+1}"]`)).toBeEnabled();await page.locator('#start').click()}
    }
    assert.equal(hunts,8,'Exactly eight explicit healthy/broken runs');
    await page.screenshot({path:path.join(output,'game-complete.png'),fullPage:true});
    await page.locator('[data-level="0"]').click();
    await page.locator('#start').click();
    await tapTarget((await targets())[0]);
    await page.locator('#broken').uncheck();
    await page.locator('#watch').click();
    await expect(page.locator('#timeline')).toContainText('score 0 → 1',{timeout:30000});
    const image=await page.locator('#run-picture').getAttribute('src');
    await page.locator('#cancel').click();
    await expect(page.locator('#error')).toContainText('Stopped. No passing result.');
    const cancelDir=path.join('.context',image.split('/')[2]);
    await expect.poll(()=>fs.existsSync(path.join(cancelDir,'summary.json')),{timeout:15000}).toBe(true);
    const canceled=JSON.parse(fs.readFileSync(path.join(cancelDir,'summary.json'),'utf8'));
    assert.equal(canceled.outcome,'incomplete');
    assert.equal(canceled.receipts[0].status,'canceled','Cancel must stop the owned runner attempt');
    await page.locator('#back').click();
    await expect(page.locator('#counter')).toHaveText('1 / 9 cats found');
    // A practice peek cannot unlock a level. Mobile zoom stays within the page.
    await page.reload();await expect(page.locator('#watch')).toBeEnabled();
    await page.setViewportSize({width:390,height:844});
    await page.locator('#start').click();await page.locator('#reveal').click();
    await expect(page.frameLocator('#scene').locator('.found')).toHaveCount(9);
    await expect(page.locator('[data-level="1"]')).toBeDisabled();
    await page.locator('#reset').click();
    await page.locator('#zoom').click();
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
    await page.locator('#zoom').click();
    await page.screenshot({path:path.join(output,'mobile-game.png'),fullPage:true});
    // Fake browser time exercises the optional real countdown; no test retries.
    await page.clock.install();
    await page.locator('#timed').check();await page.locator('#start').click();
    await expect(page.locator('#clock')).toHaveText('75s left');
    await page.clock.fastForward(76000);
    await expect(page.locator('#card-title')).toHaveText('Time for another try?');
    await expect(page.locator('#scene')).toHaveCSS('pointer-events','none');
    await page.screenshot({path:path.join(output,'mobile-timeout.png'),fullPage:true});
    // UI fixtures only: provider availability never makes a request on page load,
    // and a malformed "passed" summary must not paint a green result.
    await page.route('**/api/config',route=>route.fulfill({json:{live:true,images:true,remaining:6,runsRemaining:20}}));
    await page.reload();await expect(page.locator('#engine-mode')).toHaveText('Live Jev');
    await expect(page.locator('#look-title')).toHaveText('Jev looks');
    await expect(page.locator('#engine-note')).toContainText('6 left');
    await page.screenshot({path:path.join(output,'mobile-live-jev-ui-fixture.png'),fullPage:true});
    await page.setViewportSize({width:1440,height:1050});
    await page.screenshot({path:path.join(output,'live-jev-ui-fixture.png'),fullPage:true});
    let fakeCalls=0;
    await page.route('**/api/hunt',route=>{fakeCalls++;assert.equal(route.request().postDataJSON().live,true,'Configured Jev is automatic on an explicit watch click');return route.fulfill({json:{outcome:'passed',receipts:[{status:'passed',validated:false,executedTests:1}]}})});
    assert.equal(fakeCalls,0);await page.locator('#watch').click();
    await expect(page.locator('#run-title')).toHaveText('The run did not finish');
    await expect(page.locator('#receipt')).toBeHidden();
    assert.equal(fakeCalls,1);
    // Watching must preserve the player's start, practice and timeout screens.
    await page.locator('#back').click();
    await expect(page.locator('#card-title')).toHaveText('Ready, detective?');
    await page.locator('#start').click();await page.locator('#reveal').click();
    await page.locator('#watch').click();
    await expect(page.locator('#run-title')).toHaveText('The run did not finish');
    await page.locator('#back').click();
    await expect(page.locator('#card-title')).toHaveText('Here they are!');
    await expect(page.locator('#start')).toHaveText('Try this level again');
    await expect(page.locator('[data-level="1"]')).toBeDisabled();
    await page.locator('#reset').click();
    await page.locator('#timed').check();await page.locator('#start').click();
    await page.clock.fastForward(76000);
    await expect(page.locator('#card-title')).toHaveText('Time for another try?');
    await page.locator('#watch').click();
    await expect(page.locator('#run-title')).toHaveText('The run did not finish');
    await page.locator('#back').click();
    await expect(page.locator('#card-title')).toHaveText('Time for another try?');
    await expect(page.locator('#start')).toHaveText('Try this level again');
    // A stopped visual selection must explain that no click check ran. It is
    // still a failed receipt, never a passing result or a claimed game defect.
    await page.unroute('**/api/hunt');
    await page.route('**/api/hunt/progress?*', route => route.fulfill({json:{stage:'selection-stopped',found:0,checked:0,message:'Jev could not choose every spot confidently. No clicks were made.'}}));
    await page.route('**/api/hunt', route => route.fulfill({json:{outcome:'failed',receipts:[{status:'failed',validated:true,executedTests:1}]}}));
    await page.locator('#watch').click();
    await expect(page.locator('#run-title')).toHaveText('Jev needs your help');
    await expect(page.locator('#run-copy')).toContainText('No clicks were made.');
    await expect(page.locator('#run-result')).toHaveAttribute('data-outcome','incomplete');
    await expect(page.locator('#counter')).toContainText('0 / 9');
    await page.unroute('**/api/hunt/progress?*');
    await page.unroute('**/api/config');
    await page.route('**/api/config',route=>route.fulfill({json:{live:true,images:true,remaining:0,runsRemaining:20}}));
    const usedCalls=fakeCalls;await page.reload();
    await expect(page.locator('#engine-mode')).toHaveText('Live Jev');
    await expect(page.locator('#watch')).toBeDisabled();
    await expect(page.locator('#engine-note')).toContainText('No Jev looks left');
    assert.equal(fakeCalls,usedCalls,'Budget exhaustion must not silently switch to a free run');
    await page.unroute('**/api/hunt');await page.unroute('**/api/config');
    await page.goto(origin);
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
    console.log('PASS: four progressive hunt levels, optional timer, decoys, separate player score, manual bug/repaired scores and mode resets on all four levels, eight real healthy/broken 9lives runs, assertion-attributed failures, real owned cancellation, mobile/zoom, malformed evidence rejection; independent order assertions survive changed/misleading messages and reject wrong totals; no status labels; live shop fit at five widths, stable capture/button alignment, real failed and successful checkout assertions, saved evidence, offline continuation, theme persistence, mobile layout, cancellation controls; synthetic AI presentation only. No provider calls.');
    console.log('Screenshots:', output);
  } finally {
    await browser?.close();
    server.kill('SIGTERM');
    await exited;
  }
})().catch(error => { console.error(error.stack); process.exitCode = 1; });
