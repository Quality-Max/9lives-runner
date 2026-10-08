// Qualify the exact SDK tarball from a fresh consumer project and real browser.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const crypto = require('node:crypto');
const {spawnSync} = require('node:child_process');

const root = path.resolve(__dirname, '..');
const work = fs.mkdtempSync(path.join(os.tmpdir(), '9l-package-smoke-'));
const destination = path.join(root, '.context', 'npm-pack');
const manifest = JSON.parse(fs.readFileSync(path.join(root, 'packages/playwright/package.json')));
// The peer range admits unqualified releases; qualify against the workspace pin.
const qualifiedPlaywright = JSON.parse(fs.readFileSync(path.join(root, 'package.json'))).devDependencies['@playwright/test'];
const results = [];
let stage = 'pack';

function call(command, args, cwd = root, accepted = [0]) {
  const result = spawnSync(command, args, {cwd, encoding: 'utf8', timeout: 120000, maxBuffer: 4 << 20});
  // Do not emit npm/config/test logs or raw error payloads on failure.
  assert(!result.error && accepted.includes(result.status), `${path.basename(command)} qualification failed (status ${result.status})`);
  return result.stdout;
}

function run(binary, consumer, spec, sdk, expected) {
  const args = ['run', `tests/${spec}.spec.ts`, '--format', 'json', '--timeout', '30s', '--attempts', '1', '--receipt-dir', path.join(work, `receipts-${spec}`)];
  if (sdk) args.push('--sdk');
  const summary = JSON.parse(call(binary, args, consumer, [expected === 'passed' ? 0 : 1]));
  assert.equal(summary.receipts.length, 1);
  const receipt = summary.receipts[0];
  assert.equal(receipt.status, expected);
  assert.equal(receipt.validated, true);
  assert.equal(receipt.executedTests, 1);
  assert.equal(summary.complete, expected === 'passed');
  if (expected === 'failed') assert(receipt.failureCount > 0, 'business failure did not execute an assertion');
  results.push({spec, adapter: sdk ? 'sdk' : 'ordinary', status: receipt.status, validated: receipt.validated, executedTests: receipt.executedTests});
}

try {
  // An old tarball must never survive a failed qualification as a publish input.
  fs.rmSync(destination, {recursive: true, force: true});
  const packed = JSON.parse(call('npm', ['pack', '--workspace', '@9l/playwright', '--pack-destination', work, '--json']));
  assert.equal(packed.length, 1);
  assert.equal(packed[0].name, manifest.name);
  assert.equal(packed[0].version, manifest.version);
  const expectedFiles = ['LICENSE', 'NOTICE', 'README.md', 'package.json', ...['index', 'goal', 'protocol', 'reporter'].flatMap(name => [`dist/${name}.js`, `dist/${name}.d.ts`])].sort();
  assert.deepEqual(packed[0].files.map(file => file.path).sort(), expectedFiles);
  const tarball = path.join(work, packed[0].filename);
  const packedManifest = JSON.parse(call('tar', ['-xOf', tarball, 'package/package.json']));
  assert.equal(packedManifest.name, '@9l/playwright');
  assert.equal(packedManifest.version, manifest.version);
  assert.equal(packedManifest.license, 'Apache-2.0');
  for (const legal of ['LICENSE', 'NOTICE']) {
    assert.equal(call('tar', ['-xOf', tarball, `package/${legal}`]), fs.readFileSync(path.join(root, legal), 'utf8'));
  }
  const consumer = path.join(work, 'consumer');
  stage = 'consumer install';
  fs.mkdirSync(path.join(consumer, 'tests'), {recursive: true});
  fs.writeFileSync(path.join(consumer, 'package.json'), JSON.stringify({name: '9l-package-consumer', private: true, devDependencies: {'@9l/playwright': `file:${tarball}`, '@playwright/test': qualifiedPlaywright}}));
  call('npm', ['install', '--ignore-scripts', '--no-audit', '--no-fund'], consumer);
  const installed = path.join(consumer, 'node_modules/@9l/playwright');
  assert(fs.realpathSync(installed).startsWith(`${fs.realpathSync(consumer)}${path.sep}`), 'consumer resolved a workspace SDK');
  assert.equal(JSON.parse(fs.readFileSync(path.join(installed, 'package.json'))).version, manifest.version);
  fs.writeFileSync(path.join(consumer, 'playwright.config.ts'), "import {defineConfig} from '@playwright/test';\nexport default defineConfig({testDir: './tests', workers: 1, retries: 0, timeout: 15000, use: {headless: true}});\n");
  const page = `<button onclick="document.querySelector('[role=status]').textContent='Order confirmed: 1 item'">Place order</button><p role="status">Cart: 1 item</p>`;
  for (const [name, content] of [['checkout', page], ['business-failure', page.replace("Order confirmed: 1 item", "Order missing")]]) {
    fs.writeFileSync(path.join(consumer, `tests/${name}.spec.ts`), `import {test, expect} from '@9l/playwright';\ntest('checkout confirms one order', async ({page, n9l}) => {\n  await page.setContent(${JSON.stringify(content)});\n  await n9l.step('place order', async () => { await page.getByRole('button', {name: 'Place order'}).click(); });\n  await expect(page.getByRole('status')).toHaveText('Order confirmed: 1 item', {timeout: 500});\n});\n`);
  }
  fs.writeFileSync(path.join(consumer, 'tests/ordinary.spec.ts'), `import {test, expect} from '@playwright/test';\ntest('ordinary checkout confirms one order', async ({page}) => {\n  await page.setContent(${JSON.stringify(page)});\n  await page.getByRole('button', {name: 'Place order'}).click();\n  await expect(page.getByRole('status')).toHaveText('Order confirmed: 1 item');\n});\n`);
  const binary = path.join(work, '9l');
  stage = 'engine build';
  call('go', ['build', '-trimpath', '-o', binary, './cmd/9l']);
  stage = 'checkout assertions';
  run(binary, consumer, 'checkout', true, 'passed');
  stage = 'business failure assertions';
  run(binary, consumer, 'business-failure', true, 'failed');
  stage = 'ordinary Playwright assertions';
  run(binary, consumer, 'ordinary', false, 'passed');
  stage = 'artifact admission';
  fs.mkdirSync(destination, {recursive: true});
  fs.copyFileSync(tarball, path.join(destination, path.basename(tarball)));
  fs.writeFileSync(path.join(destination, 'qualification.json'), JSON.stringify({name: manifest.name, version: manifest.version, sha256: crypto.createHash('sha256').update(fs.readFileSync(tarball)).digest('hex'), results}, null, 2) + '\n');
  console.log('Packed SDK: clean install, real checkout assertions, business failure and ordinary Playwright verified');
} catch {
  fs.rmSync(destination, {recursive: true, force: true});
  console.error(`Packed SDK qualification failed at ${stage}; no package admitted for publication`);
  process.exitCode = 1;
} finally {
  fs.rmSync(work, {recursive: true, force: true});
}
