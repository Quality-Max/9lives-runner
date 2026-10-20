// All data is synthetic. Only publish a checked chapter; never turn a timeout
// or an expected failure's exit code alone into a successful demo claim.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {execFile, spawn} = require('node:child_process');
const {createInterface} = require('node:readline');
const {createHash} = require('node:crypto');
const {start} = require('./app/server.cjs');
const {render} = require('./report.cjs');

const root = path.resolve(__dirname, '../..');
const headed = process.argv.includes('--headed');
assert(process.argv.slice(2).every(arg => arg === '--headed'), 'Only --headed is supported');
fs.mkdirSync(path.join(root, '.context'), {recursive: true});
const work = fs.mkdtempSync(path.join(root, '.context/release-lab-'));
const project = path.join(work, 'project');
const receipts = path.join(work, 'receipts');
const engine = path.join(work, process.platform === 'win32' ? '9l.exe' : '9l');
const hash = value => createHash('sha256').update(value).digest('hex');
const env = {};
for (const key of ['PATH', 'HOME', 'TMPDIR', 'TEMP', 'TMP', 'SystemRoot', 'USERPROFILE', 'LOCALAPPDATA', 'APPDATA', 'PLAYWRIGHT_BROWSERS_PATH']) {
  if (process.env[key] !== undefined) env[key] = process.env[key];
}
if (headed) for (const key of ['DISPLAY', 'WAYLAND_DISPLAY', 'XAUTHORITY']) {
  if (process.env[key] !== undefined) env[key] = process.env[key];
}
env.LAB_HEADED = headed ? '1' : '0';
const summary = {version: 1, status: 'running', startedAt: new Date().toISOString(), platform: `${process.platform}/${process.arch}`, revision: '', chapters: []};
let stage = 'prerequisites';
let app;
const cancellation = new AbortController();
for (const signal of ['SIGINT', 'SIGTERM']) process.once(signal, () => cancellation.abort());

function invoke(command, args, cwd = project, expected = 0, extraEnv = {}) {
  return new Promise((resolve, reject) => {
    const child = execFile(command, args, {cwd, env: {...env, ...extraEnv}, signal: cancellation.signal, encoding: 'utf8', timeout: 240000, maxBuffer: 8 << 20, killSignal: 'SIGTERM'}, (error, stdout) => {
      const code = error ? error.code : 0;
      if (code !== expected) return reject(new Error(`command exit ${code}; expected ${expected}`));
      resolve(stdout);
    });
    child.stdin.end();
  });
}
const cli = async (args, expected = 0, extraEnv = {}, cwd = project) => JSON.parse(await invoke(engine, args, cwd, expected, extraEnv));
function save(name, data) { fs.writeFileSync(path.join(work, name), JSON.stringify(data, null, 2) + '\n'); return name; }
function publish() { save('summary.json', summary); fs.writeFileSync(path.join(work, 'index.html'), render(summary)); }
function chapter(feature, title, description, result, command, evidence) {
  summary.chapters.push({feature, title, description, result, command, evidence});
  publish();
  console.log(`${summary.chapters.length}/8  ${title}\n     ${result}`);
}
function checkRun(report, outcome, status) {
  assert.equal(report.outcome, outcome);
  assert.equal(report.receipts.length, 1);
  assert.equal(report.receipts[0].validated, true);
  assert.equal(report.receipts[0].executedTests, 1);
  assert.equal(report.receipts[0].status, status);
  return report.receipts[0];
}
async function run(spec, {expected = 0, extra = [], extraEnv = {}, sdk = true} = {}) {
  return cli(['run', `tests/${spec}.spec.ts`, ...(sdk ? ['--sdk'] : []), '--format', 'json', '--timeout', '30s', '--attempts', '1', '--receipt-dir', receipts,
    '--pass-env', 'LAB_HEADED', ...(headed ? ['--headed'] : []), ...extra], expected, extraEnv);
}
async function provenance(spec, cwd = project, name = spec) {
  const result = await cli(['provenance', `tests/${spec}.spec.ts`, '--agent', 'codex'], 0, {}, cwd);
  return save(`${name}-provenance.json`, result);
}

// Sequential JSON-RPC requests over the real stdio server. No editor setup.
async function mcpChapter(url) {
  const child = spawn(engine, ['mcp', '--provider', 'none', '--receipt-dir', receipts], {cwd: project, env, signal: cancellation.signal, stdio: ['pipe', 'pipe', 'ignore']});
  const lines = createInterface({input: child.stdout});
  let serial = 0;
  const pending = new Map();
  const stopped = new Promise(resolve => { child.once('exit', resolve); child.once('error', resolve); });
  const rejectPending = () => { for (const item of pending.values()) item.reject(new Error('MCP ended before its response')); };
  child.once('exit', rejectPending);
  child.once('error', rejectPending);
  child.stdin.on('error', rejectPending);
  lines.on('line', line => {
    let response;
    try { response = JSON.parse(line); } catch { rejectPending(); return; }
    const item = pending.get(response.id);
    if (!item) return;
    response.error ? item.reject(new Error('MCP returned a protocol error')) : item.resolve(response.result);
  });
  function request(method, params) {
    const id = ++serial;
    let timer;
    return new Promise((resolve, reject) => {
      timer = setTimeout(() => reject(new Error('MCP response timed out')), 45000);
      pending.set(id, {resolve, reject});
      child.stdin.write(JSON.stringify({jsonrpc: '2.0', id, method, params}) + '\n');
    }).finally(() => { clearTimeout(timer); pending.delete(id); });
  }
  // A single session deadline also covers shutdown and malformed responses.
  const deadline = setTimeout(() => { rejectPending(); child.kill('SIGTERM'); }, 90000);
  async function call(name, args) {
    const response = await request('tools/call', {name, arguments: args});
    assert(!response.isError, `MCP ${name} refused`);
    return JSON.parse(response.content[0].text);
  }
  try {
    await request('initialize', {protocolVersion: '2025-06-18', capabilities: {}, clientInfo: {name: '9l-release-lab', version: '1'}});
    child.stdin.write(JSON.stringify({jsonrpc: '2.0', method: 'notifications/initialized'}) + '\n');
    const tools = await request('tools/list', {});
    assert.equal(tools.tools.length, 7);
    const generated = await call('generate_playwright_repro', {url, finding: {category: 'links', url: url + 'help', message: 'Support must be reachable'}, test_name: 'support link returns a successful response', output_path: 'tests/support-repro.spec.ts'});
    // Review the deterministic template before execution. Do not call generation a pass.
    assert(generated.code.includes('expect(response.status()).toBeLessThan(400)'));
    assert(generated.code.includes(JSON.stringify(url + 'help')));
    await provenance('support-repro');
    const broken = await call('run_test', {spec: 'tests/support-repro.spec.ts', run_timeout: 30});
    assert.equal(broken.status, 'failed');
    assert.equal(broken.executedTests, 1);
    assert.equal(broken.failureCount, 1);
    app.setQuality('clean');
    const fixed = await call('run_test', {spec: 'tests/support-repro.spec.ts', run_timeout: 30});
    assert.equal(fixed.status, 'passed');
    assert.equal(fixed.executedTests, 1);
    for (const result of [broken, fixed]) {
      const receipt = JSON.parse(fs.readFileSync(result.receipt, 'utf8'));
      assert.equal(receipt.validated, true);
    }
    return {tools: tools.tools.map(tool => tool.name), generated, broken, fixed};
  } finally {
    child.stdin.end();
    await stopped;
    clearTimeout(deadline);
    lines.close();
  }
}

async function main() {
  assert(fs.existsSync(path.join(root, 'node_modules/@playwright/test/package.json')), 'Run npm ci first');
  console.log('9l release lab — local shop, intentional defects, real Chromium\nBuilding local packages and runner…');
  await invoke(process.platform === 'win32' ? 'npm.cmd' : 'npm', ['run', 'build'], root);
  await invoke('go', ['build', '-o', engine, './cmd/9l'], root);
  summary.revision = (await invoke('git', ['rev-parse', 'HEAD'], root)).trim();
  fs.mkdirSync(project);
  for (const file of ['app', 'tests', 'playwright.config.ts', 'package.json', 'requirements.json', 'goal-script.json', 'policy-script.json']) {
    fs.cpSync(path.join(__dirname, file), path.join(project, file), {recursive: true});
  }
  for (const spec of ['checkout', 'checkout-complete', 'ordinary', 'goal', 'policy', 'stale-locator']) await provenance(spec);
  app = await start();
  const {chromium} = require('playwright');
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({viewport: {width: 1280, height: 900}});
    await page.goto(app.url);
    await page.getByText('Pencil set', {exact: true}).waitFor();
    await page.screenshot({path: path.join(work, 'shop.png'), fullPage: true});
  } finally { await browser.close(); }
  publish();

  stage = 'scan and inspect';
  const scanArgs = ['scan', app.url, '--allow-private-network', '--format', 'json'];
  const baseline = await cli(scanArgs);
  const baselineFile = path.join(project, 'baseline.json');
  fs.writeFileSync(baselineFile, JSON.stringify(baseline));
  app.setQuality('broken');
  const regression = await cli([...scanArgs, '--baseline', 'baseline.json', '--fail-on-new'], 1);
  assert(regression.delta.new.some(finding => finding.category === 'accessibility' && finding.selector === '#promo'));
  assert(regression.delta.new.some(finding => finding.category === 'links'));
  const inspection = await cli(['inspect', app.url, '--allow-private-network', '--accessibility-tree', '--format', 'json']);
  assert(JSON.stringify(inspection).includes('Continue to review'));
  chapter('scan + inspect', 'Catch a regression before writing a test', 'Nine checks observe one local page load. Removing a label and breaking support creates new findings against the clean baseline.',
    'New label and link findings detected; baseline gate exited 1.', '9l scan <local-url> --allow-private-network --format json\n9l scan <local-url> --allow-private-network --baseline baseline.json --fail-on-new\n9l inspect <local-url> --allow-private-network --accessibility-tree', [save('scan-baseline.json', baseline), save('scan-regression.json', regression), save('inspect.json', inspection)]);

  stage = 'assessment';
  const assessment = await cli(['assess', 'tests/checkout.spec.ts', '--requirements', 'requirements.json', '--agent-provenance', path.join(work, 'checkout-provenance.json'), '--format', 'json']);
  assert(assessment.tests.some(test => test.findings.some(finding => finding.rule === 'unmapped-outcome' && finding.outcome === 'recommendation')));
  chapter('assess + provenance', 'Ask what the green test actually protects', 'The independent demo contract includes recommendations. The draft maps cart and order assertions, leaving one suspected gap.',
    'Unmapped outcome found; source hash and declared authorship recorded.', '9l provenance tests/checkout.spec.ts --agent codex\n9l assess tests/checkout.spec.ts --requirements requirements.json --format json', [save('assessment.json', assessment), 'checkout-provenance.json']);

  stage = 'ordinary and SDK execution';
  const ordinary = await run('ordinary', {sdk: false});
  const sdk = await run('checkout');
  assert.equal(checkRun(ordinary, 'passed', 'passed').adapter, 'playwright');
  assert.equal(checkRun(sdk, 'passed', 'passed').adapter, 'playwright-sdk');
  chapter('run + steps + receipts', 'Keep Playwright. Add attributable execution.', 'Run an ordinary Playwright spec and the SDK version with an explicit 9l step. Both assert the two-item order in Chromium.',
    'Both adapters passed, each with 1 executed test and a validated receipt.', '9l run tests/ordinary.spec.ts\n9l run tests/checkout.spec.ts --sdk' + (headed ? ' --headed' : ''), [save('ordinary.json', ordinary), save('sdk.json', sdk)]);

  stage = 'goals and stop policy';
  const goalArgs = ['--goal-script', 'goal-script.json'];
  const goal = await run('goal', {extra: goalArgs});
  assert.equal(checkRun(goal, 'passed', 'passed').goals[0].status, 'completed');
  const defective = await run('goal', {expected: 1, extra: [...goalArgs, '--pass-env', 'LAB_PROFILE_BUG'], extraEnv: {LAB_PROFILE_BUG: '1'}});
  assert.equal(checkRun(defective, 'failed', 'failed').goals[0].status, 'completed');
  const policy = await run('policy', {expected: 3, extra: ['--goal-script', 'policy-script.json']});
  assert.equal(policy.outcome, 'incomplete');
  assert.equal(policy.receipts.length, 1);
  assert.equal(policy.receipts[0].validated, true);
  assert.equal(policy.receipts[0].executedTests, 1);
  assert.equal(policy.receipts[0].goals[0].status, 'policy_blocked');
  assert.equal(policy.receipts[0].nonGoalFailureCount || 0, 0);
  chapter('experimental goals', 'A completed goal is not a verified outcome', 'A bounded offline script fills the form. A planted profile defect still fails the business assertion. A separate purchase goal stops before the request.',
    'Good form passed; defective form failed; purchase goal was blocked.', '9l run tests/goal.spec.ts --sdk --goal-script goal-script.json\n9l run tests/policy.spec.ts --sdk --goal-script policy-script.json', [save('goal.json', goal), save('goal-defect.json', defective), save('goal-policy.json', policy)]);

  stage = 'offline healing';
  const sourcePath = path.join(project, 'tests/stale-locator.spec.ts');
  const source = fs.readFileSync(sourcePath, 'utf8');
  const healed = await cli(['heal', 'tests/stale-locator.spec.ts', '--provider', 'none', '--run-timeout', '30']);
  assert.equal(healed.state, 'verified');
  assert.equal(healed.original.passed, false);
  assert.equal(healed.original.executedTests, 1);
  assert.equal(healed.verified.passed, true);
  assert.equal(healed.verified.executedTests, 1);
  assert.equal(healed.providerCalls, 0);
  assert.equal(healed.applied, false);
  const candidate = fs.readFileSync(sourcePath + '.healed', 'utf8');
  assert.equal(candidate, source.replace('name: "Continue now"', 'name: "Continue to review"'));
  assert.equal(fs.readFileSync(sourcePath, 'utf8'), source);
  assert.equal(healed.originalHash, hash(source));
  assert.equal(healed.candidateHash, hash(candidate));
  chapter('experimental heal', 'Repair a locator. Preserve the assertion.', 'A seeded stale button name fails. Offline Tier 1 proposes the observed button, then verifies the candidate in isolation.',
    'One locator literal changed; candidate passed; original untouched.', '9l heal tests/stale-locator.spec.ts --provider none', [save('heal.json', healed), 'project/tests/stale-locator.spec.ts.healed']);

  stage = 'network fault proof';
  // Reserve then release a loopback port: every isolated worker must use the
  // same origin as discovery. An unlikely intervening bind fails closed.
  const reservation = await start();
  const port = new URL(reservation.url).port;
  await reservation.close();
  const proofArgs = ['--format', 'json', '--paths', '--timeout', '30s', '--pass-env', 'LAB_PORT', '--receipt-dir', receipts];
  const weak = await cli(['prove', 'tests/checkout.spec.ts', ...proofArgs], 0, {LAB_PORT: port});
  const strong = await cli(['prove', 'tests/checkout-complete.spec.ts', ...proofArgs], 0, {LAB_PORT: port});
  for (const [report, caught, survived] of [[weak, 6, 3], [strong, 9, 0]]) {
    assert.equal(report.complete, true);
    assert.equal(report.baseline.status, 'passed');
    assert.deepEqual(report.summary, {faults: 9, caught, survived, inconclusive: 0, notRun: 0, exercised: 9});
    const recommendation = report.requests.find(request => new URL(request.url).pathname === '/api/recommendations');
    assert(recommendation);
    assert.deepEqual(report.faults.filter(fault => fault.request === recommendation.id).map(fault => fault.result), Array(3).fill(survived ? 'survived' : 'caught'));
    for (const runId of [report.baseline.runId, ...report.faults.map(fault => fault.runId)]) {
      const result = await cli(['result', runId, '--receipt-dir', receipts]);
      assert.equal(result.receipts[0].validated, true);
      assert.equal(result.receipts[0].executedTests, 1);
    }
  }
  chapter('experimental prove', 'Break the network. Find the missing assertion.', 'Abort, HTTP 500, and empty JSON are injected into cart, orders, and recommendations. Then the same faults run against the stronger test.',
    'Before: 6 caught / 3 survived. After: 9 caught / 0 survived.', '9l prove tests/checkout.spec.ts --pass-env LAB_PORT\n9l prove tests/checkout-complete.spec.ts --pass-env LAB_PORT', [save('prove-before.json', weak), save('prove-after.json', strong)]);

  stage = 'revision confirmation';
  const repo = path.join(work, 'confirmation');
  fs.mkdirSync(repo);
  for (const file of ['app', 'playwright.config.ts', 'package.json']) fs.cpSync(path.join(project, file), path.join(repo, file), {recursive: true});
  fs.mkdirSync(path.join(repo, 'tests'));
  for (const file of ['fixtures.ts', 'checkout-complete.spec.ts']) fs.copyFileSync(path.join(project, 'tests', file), path.join(repo, 'tests', file));
  fs.writeFileSync(path.join(repo, '.gitignore'), 'node_modules/\ntest-results/\n.9lives/\n');
  // Disable user hooks/signing only in this synthetic repository.
  const git = async (...args) => (await invoke('git', ['-c', 'user.name=9l demo', '-c', 'user.email=demo@example.invalid', '-c', 'commit.gpgsign=false', '-c', 'core.hooksPath=' + path.join(work, 'no-hooks'), ...args], repo)).trim();
  const orderFile = path.join(repo, 'app/order.cjs');
  const fixedCode = fs.readFileSync(orderFile, 'utf8');
  fs.writeFileSync(orderFile, fixedCode.replace('items.length;', 'items.length - 1;'));
  await git('init', '--quiet');
  await git('config', 'core.hooksPath', path.join(work, 'no-hooks'));
  await git('add', '.');
  await git('commit', '--quiet', '-m', 'Seed wrong order count');
  const unfixed = await git('rev-parse', 'HEAD');
  fs.writeFileSync(orderFile, fixedCode);
  await git('commit', '--quiet', '-am', 'Count every item');
  const fixed = await git('rev-parse', 'HEAD');
  fs.symlinkSync(path.join(root, 'node_modules'), path.join(repo, 'node_modules'), 'junction');
  try {
    await provenance('checkout-complete', repo, 'confirm');
    const confirmation = await cli(['confirm', 'tests/checkout-complete.spec.ts', '--unfixed', unfixed, '--fixed', fixed, '--format', 'json', '--timeout', '30s', '--receipt-dir', receipts], 0, {}, repo);
    assert.equal(confirmation.verdict, 'confirmed');
    assert.deepEqual(confirmation.tests.map(test => `${test.unfixed}/${test.fixed}:${test.result}`), ['assertion-failed/passed:confirmed']);
    for (const runId of [confirmation.unfixed.runId, confirmation.fixed.runId]) {
      const result = await cli(['result', runId, '--receipt-dir', receipts]);
      assert.equal(result.receipts[0].validated, true);
      assert.equal(result.receipts[0].executedTests, 1);
    }
    assert.equal(((await git('worktree', 'list', '--porcelain')).match(/^worktree /gm) || []).length, 1);
    chapter('experimental confirm', 'Prove the fix changes the outcome', 'The same browser repro runs against two commits of this shop in isolated worktrees. The old code counts one item; the fix counts two.',
      'Confirmed: assertion failed on the old commit and passed on the fix.', '9l confirm tests/checkout-complete.spec.ts --unfixed <bug-commit> --fixed <fix-commit>', [save('confirm.json', confirmation)]);
  } finally { fs.unlinkSync(path.join(repo, 'node_modules')); }

  stage = 'MCP generated reproduction';
  const mcp = await mcpChapter(app.url);
  chapter('native MCP', 'Turn a scan finding into an executed repro', 'The real stdio MCP server generates a support-link assertion. We review its template, run it on the broken link, repair the local fixture, and rerun.',
    'Seven tools discovered; generated repro failed, then passed after repair.', 'MCP: tools/list → generate_playwright_repro → run_test\nRepair local support route → run_test', [save('mcp.json', mcp), 'project/tests/support-repro.spec.ts']);
  stage = 'report export';
  summary.status = 'passed';
  summary.finishedAt = new Date().toISOString();
  publish();
  const reportBrowser = await chromium.launch();
  try {
    const page = await reportBrowser.newPage({viewport: {width: 1280, height: 1000}});
    await page.goto(require('node:url').pathToFileURL(path.join(work, 'index.html')).href);
    await page.screenshot({path: path.join(work, 'report.png'), fullPage: true});
  } finally { await reportBrowser.close(); }
  console.log(`\nAll eight chapters checked.\nReport: ${path.join(work, 'index.html')}\nEvidence: ${work}`);
}

main().catch(error => {
  summary.status = 'failed';
  summary.failedStage = stage;
  publish();
  // Raw child stdout/stderr can contain page data. Keep failure reporting bounded.
  console.error(`Demo stopped at ${stage} (${error instanceof assert.AssertionError ? 'expectation mismatch' : 'execution error'}). No overall pass claimed.\nPartial evidence: ${work}`);
  process.exitCode = 1;
}).finally(async () => { if (app) await app.close(); });
