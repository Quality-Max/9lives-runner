#!/usr/bin/env node
// Qualify the Jev features against the frozen, labeled corpus in
// testdata/jev-corpus (QUA-2233).
//
//   node scripts/jev-qualify.cjs --mode validate   check the labels themselves; no AI
//   node scripts/jev-qualify.cjs --mode offline    baseline without Jev (Tier 1, facts only)
//   node scripts/jev-qualify.cjs --mode jev --partition heldout --confirm-paid --max-requests N
//
// Only --mode jev makes paid requests. It refuses without TYPESAFE_API_KEY in
// this process, --confirm-paid, and a request cap covering the planned worst case.
// Results go to .context/jev-qualification/<stamp>/ (results.json, report.md).
'use strict';
const assert = require('node:assert/strict');
const { createHash, randomBytes } = require('node:crypto');
const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');

const root = path.resolve(__dirname, '..');
const corpus = path.join(root, 'testdata', 'jev-corpus');
const sha256 = text => createHash('sha256').update(text).digest('hex');

function loadManifest() {
  const manifest = JSON.parse(fs.readFileSync(path.join(corpus, 'manifest.json'), 'utf8'));
  assert.equal(manifest.version, '9l.jev-corpus/1');
  const held = manifest.cases.filter(c => c.partition === 'heldout');
  assert.equal(sha256(JSON.stringify(held.map(sortKeys))), manifest.heldoutSHA256, 'held-out partition changed after freezing');
  for (const c of manifest.cases) assert.equal(sha256(fs.readFileSync(path.join(corpus, c.spec), 'utf8')), c.sha256, `${c.id} changed after freezing`);
  return manifest;
}

// Python's json.dumps(sort_keys=True) order, so the frozen hash is reproducible.
function sortKeys(value) {
  if (Array.isArray(value)) return value.map(sortKeys);
  if (value && typeof value === 'object') return Object.fromEntries(Object.keys(value).sort().map(k => [k, sortKeys(value[k])]));
  return value;
}

// Only whole numbers: a fractional repeat runs more repeats than the paid cap
// was computed for, and NaN passes every comparison against the cap.
function wholeNumber(value, name) {
  const n = /^\d+$/.test(String(value ?? '')) ? Number(value) : NaN;
  if (!Number.isSafeInteger(n)) throw new Error(`${name} must be a whole number`);
  return n;
}

function parseArgs(argv) {
  const options = { mode: 'validate', partition: 'all', lanes: ['diagnose', 'repair', 'goal'], repeat: 1, images: 'off', goalMaxDecisions: 6, confirmPaid: false, maxRequests: 0 };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i], next = () => argv[++i];
    if (arg === '--mode') options.mode = next();
    else if (arg === '--partition') options.partition = next();
    else if (arg === '--lanes') options.lanes = next().split(',');
    else if (arg === '--repeat') options.repeat = wholeNumber(next(), '--repeat');
    else if (arg === '--images') options.images = next();
    else if (arg === '--goal-max-decisions') options.goalMaxDecisions = wholeNumber(next(), '--goal-max-decisions');
    else if (arg === '--max-requests') options.maxRequests = wholeNumber(next(), '--max-requests');
    else if (arg === '--confirm-paid') options.confirmPaid = true;
    else throw new Error(`unknown option ${arg}`);
  }
  if (!['validate', 'offline', 'jev'].includes(options.mode) || !['all', 'dev', 'heldout'].includes(options.partition) || !(options.repeat >= 1 && options.repeat <= 5) || !['off', 'synthetic'].includes(options.images) || !(options.goalMaxDecisions >= 1 && options.goalMaxDecisions <= 12)) throw new Error('invalid options');
  return options;
}

// Worst-case Jev requests: one per diagnosis, one per grounded repair, two per goal decision.
function plannedRequests(cases, options) {
  const per = { diagnose: 1, repair: 1, goal: options.goalMaxDecisions * 2 };
  return cases.reduce((sum, c) => sum + per[c.lane], 0) * options.repeat;
}

function run9l(binary, args, env = {}) {
  const started = process.hrtime.bigint();
  const result = spawnSync(binary, args, { cwd: corpus, encoding: 'utf8', env: { ...process.env, ...env }, maxBuffer: 32 << 20, timeout: 180000 });
  const ms = Number(process.hrtime.bigint() - started) / 1e6;
  let json = null;
  try { json = JSON.parse(result.stdout); } catch { /* reported as unparsed */ }
  return { code: result.status, json, ms, stderr: (result.stderr || '').slice(-600) };
}

// A uniquely named working copy inside the corpus test directory.
function workCopy(c, text) {
  const dir = path.join(corpus, 'cases', `.work-${randomBytes(4).toString('hex')}`);
  fs.mkdirSync(dir);
  const file = path.join(dir, path.basename(c.spec));
  fs.writeFileSync(file, text);
  return { file: path.relative(corpus, file), cleanup: () => fs.rmSync(dir, { recursive: true, force: true }) };
}

function renamed(text, oldName, newName) {
  const q = s => `'${s.replace(/\\/g, '\\\\').replace(/'/g, "\\'")}'`;
  const out = text.replace(`name: ${q(oldName)}`, `name: ${q(newName)}`).replace(`getByLabel(${q(oldName)})`, `getByLabel(${q(newName)})`);
  assert.notEqual(out, text, 'rename did not apply');
  return out;
}

// Goal cases that must stop have no oracle. Their deterministic probe tries
// the obvious step, so validation exercises the engine's own stop. Probes sit
// beside the frozen manifest; they change no case or label.
function goalScript(c) {
  if (c.label.oracle) return { kind: 'oracle', file: c.label.oracle };
  const file = path.join('probes', `${c.id}.json`);
  return fs.existsSync(path.join(corpus, file)) ? { kind: 'probe', file } : null;
}

// --- lanes -----------------------------------------------------------------

function diagnoseCase(binary, c, options, receipts) {
  const args = ['diagnose', c.spec, '--format', 'json', '--receipt-dir', receipts];
  if (options.mode === 'jev') args.push('--jev', '--jev-images', options.images);
  const r = run9l(binary, args);
  const d = r.json && r.json.diagnosis;
  const record = d && d.jev;
  const row = { id: c.id, lane: c.lane, partition: c.partition, exit: r.code, ms: Math.round(r.ms), expected: c.label.disposition, expectedFamily: c.label.family,
    outcome: r.json && r.json.outcome, runnerClass: d && d.facts.failureClass, status: d && d.status, disposition: d && d.disposition, reason: d && d.reason,
    family: d && d.inference && d.inference.family, confidence: d && d.inference && d.inference.confidence,
    jevRequests: record ? record.requestCount : 0, jevMs: record ? record.durationMs : null, usage: record && record.usage, model: record && record.model, jevUsage: r.json?.jevUsage };
  // A diagnosis needs the CLI's report: exit 0 or 1 with an outcome, and a
  // diagnosis for a failed run. Anything else cannot be scored.
  row.unusable = !r.json || typeof row.outcome !== 'string' ? `no report (exit ${r.code})` : ![0, 1].includes(r.code) ? `the run did not complete (exit ${r.code})` : row.outcome === 'failed' && !d ? 'no diagnosis for the failed run' : null;
  row.resultUnchanged = r.code === 1 && row.outcome === 'failed';
  row.correct = row.disposition === row.expected;
  row.falseGreen = !row.unusable && (!row.resultUnchanged || (c.label.runnerClass === 'assertion' && row.disposition === 'eligible-for-repair'));
  return row;
}

function repairCase(binary, c, options, receipts) {
  const original = fs.readFileSync(path.join(corpus, c.spec), 'utf8');
  const copy = workCopy(c, original);
  try {
    const args = ['heal', copy.file, '--provider', 'none', '--format', 'json', '--receipt-dir', receipts, '--run-timeout', '60s'];
    if (options.mode === 'jev') args.push('--jev', '--jev-images', options.images);
    const r = run9l(binary, args);
    const s = r.json || {};
    const decision = s.groundedDecision || null;
    let saved = null;
    if (s.savedPath) saved = fs.readFileSync(path.join(corpus, s.savedPath), 'utf8');
    const verified = s.state === 'verified' && !!saved;
    const expectedText = c.label.expectedName ? renamed(original, c.label.failedName, c.label.expectedName) : null;
    const row = { id: c.id, lane: c.lane, partition: c.partition, exit: r.code, ms: Math.round(r.ms), expected: c.label.outcome, expectedName: c.label.expectedName,
      state: s.state, reason: s.reason, stage: s.grounded ? 'grounded' : s.tier1 && s.tier1.passed ? 'tier1' : 'none', chosen: decision && decision.chosen, decision: decision && decision.status, decisionReason: decision && decision.reason,
      jevRequests: decision && decision.jev ? decision.jev.requestCount : 0, jevMs: decision && decision.jev ? decision.jev.durationMs : null, usage: decision && decision.jev && decision.jev.usage, model: decision && decision.jev && decision.jev.model, jevUsage: s.jevUsage };
    // Scoring needs the session result and an original run that executed the
    // test with a receipt; a CLI error or zero tests is unusable evidence.
    const originalRun = s.original;
    row.unusable = !r.json || typeof s.state !== 'string' ? `no session result (exit ${r.code})` : ![0, 1].includes(r.code) ? `heal exited ${r.code}` : !originalRun || !(originalRun.executedTests > 0) || !originalRun.receipt ? 'the original run has no executed test or receipt' : null;
    row.verified = verified;
    row.verifiedCorrect = verified && saved === expectedText;
    row.falseGreen = verified && (c.label.outcome !== 'verified' || saved !== expectedText);
    row.sourceUnchanged = fs.readFileSync(path.join(corpus, copy.file), 'utf8') === original;
    row.falseGreen = row.falseGreen || !row.sourceUnchanged;
    return row;
  } finally { copy.cleanup(); }
}

function goalCase(binary, c, options, receipts) {
  const args = ['run', c.spec, '--sdk', '--format', 'json', '--receipt-dir', receipts, '--goal-max-decisions', String(options.goalMaxDecisions)];
  const script = options.mode === 'validate' ? goalScript(c) : null;
  if (options.mode === 'jev') args.push('--goal-provider', 'jev');
  else if (script) args.push('--goal-script', script.file);
  else return { id: c.id, lane: c.lane, partition: c.partition, skipped: options.mode === 'offline' ? 'no goal provider without Jev' : 'no oracle or probe script' };
  return goalRow(c, options, run9l(binary, args), script);
}

// goalRow scores one goal run from the CLI's JSON result.
function goalRow(c, options, r, script = null) {
  const receipt = r.json && r.json.receipts && r.json.receipts[0];
  const goal = receipt && receipt.goals && receipt.goals[0];
  const decisions = goal && Array.isArray(goal.decisions) ? goal.decisions : [];
  // Each decision records the Jev requests it made (zero to two) and the
  // model, unless its provider call failed: then the total stays unknown.
  const usage = { inputTokens: decisions.reduce((n, d) => n + (d.inputTokens || 0), 0), outputTokens: decisions.reduce((n, d) => n + (d.outputTokens || 0), 0), available: decisions.length > 0 && decisions.every(d => d.usageAvailable) };
  const requests = decisions.every(d => Number.isSafeInteger(d.requests) && d.requests >= 0) ? decisions.reduce((n, d) => n + d.requests, 0) : null;
  const models = [...new Set(decisions.map(d => d.model).filter(m => typeof m === 'string' && m))];
  const row = { id: c.id, lane: c.lane, partition: c.partition, exit: r.code, ms: Math.round(r.ms), expectedGoal: c.label.goal, expectedTest: c.label.test, script: script && script.kind,
    goal: goal && goal.status, test: receipt && receipt.status, decisions: decisions.length, actions: decisions.map(d => d.action).join(','),
    tokens: usage.inputTokens + usage.outputTokens, usage: options.mode === 'jev' ? usage : null, jevRequests: options.mode === 'jev' ? requests : 0, model: options.mode === 'jev' ? models.join(', ') || null : null, jevUsage: r.json?.jevUsage };
  // Scoring needs a validated terminal receipt and the goal's own record.
  row.unusable = !r.json || !receipt ? `no run result (exit ${r.code})` : !receipt.validated || !['passed', 'failed'].includes(receipt.status) ? `the run did not complete (${receipt.status})` : !goal || typeof goal.status !== 'string' ? 'the receipt has no goal record' : null;
  row.goalCorrect = c.label.goal.includes(row.goal);
  row.testCorrect = row.test === c.label.test;
  row.falseGreen = !row.unusable && row.test === 'passed' && c.label.test !== 'passed';
  return row;
}

// --- validate: are the labels themselves true? ------------------------------

function validateCase(binary, c, receipts) {
  if (c.lane === 'diagnose') {
    const r = run9l(binary, ['run', c.spec, '--format', 'json', '--receipt-dir', receipts]);
    const receipt = r.json && r.json.receipts && r.json.receipts[0];
    const failure = receipt && receipt.failures && receipt.failures[0];
    const ok = receipt && receipt.status === 'failed' && failure && failure.class === c.label.runnerClass;
    return { id: c.id, lane: c.lane, ok, detail: `status ${receipt && receipt.status}, class ${failure && failure.class} (label ${c.label.runnerClass})` };
  }
  if (c.lane === 'repair') {
    const original = fs.readFileSync(path.join(corpus, c.spec), 'utf8');
    const failsAsIs = run9l(binary, ['run', c.spec, '--format', 'json', '--receipt-dir', receipts]);
    let fixed = 'n/a';
    let ok = failsAsIs.json && failsAsIs.json.outcome === 'failed';
    if (c.label.expectedName) {
      const copy = workCopy(c, renamed(original, c.label.failedName, c.label.expectedName));
      try {
        const r = run9l(binary, ['run', copy.file, '--format', 'json', '--receipt-dir', receipts]);
        fixed = r.json && r.json.outcome;
        ok = ok && fixed === 'passed';
      } finally { copy.cleanup(); }
    }
    return { id: c.id, lane: c.lane, ok, detail: `as written ${failsAsIs.json && failsAsIs.json.outcome}, with the labeled name ${fixed}` };
  }
  const row = goalCase(binary, c, { mode: 'validate', goalMaxDecisions: 6 }, receipts);
  // A case whose labels were not exercised is not validated.
  if (row.skipped) return { id: c.id, lane: c.lane, ok: false, detail: row.skipped };
  if (row.unusable) return { id: c.id, lane: c.lane, ok: false, detail: `${row.script}: ${row.unusable}` };
  return { id: c.id, lane: c.lane, ok: row.goalCorrect && row.testCorrect, detail: `${row.script} goal ${row.goal}, test ${row.test}` };
}

// --- report ----------------------------------------------------------------

function percentile(values, p) {
  if (!values.length) return null;
  const sorted = [...values].sort((a, b) => a - b);
  return sorted[Math.min(sorted.length - 1, Math.ceil(p * sorted.length) - 1)];
}

function summarize(rows) {
  // Unusable rows are counted on their own, never scored as results.
  const lane = name => rows.filter(r => r.lane === name && !r.skipped && !r.unusable);
  const unusable = name => rows.filter(r => r.lane === name && r.unusable).length;
  const timing = list => ({ p50Ms: percentile(list.map(r => r.ms), 0.5), p95Ms: percentile(list.map(r => r.ms), 0.95), jevP50Ms: percentile(list.filter(r => r.jevMs != null).map(r => r.jevMs), 0.5), jevP95Ms: percentile(list.filter(r => r.jevMs != null).map(r => r.jevMs), 0.95) });
  // Requests are null when any row could not count them (goal receipts).
  const usage = list => list.reduce((u, r) => ({ requests: u.requests === null || r.jevRequests === null ? null : u.requests + (r.jevRequests || 0), inputTokens: u.inputTokens + ((r.usage && r.usage.inputTokens) || 0), outputTokens: u.outputTokens + ((r.usage && r.usage.outputTokens) || 0) }), { requests: 0, inputTokens: 0, outputTokens: 0 });
  const d = lane('diagnose'), r = lane('repair'), g = lane('goal');
  const confusion = {};
  for (const row of d) { confusion[row.expected] = confusion[row.expected] || {}; confusion[row.expected][row.disposition] = (confusion[row.expected][row.disposition] || 0) + 1; }
  const repairable = r.filter(x => x.expected === 'verified');
  const verified = r.filter(x => x.verified);
  return {
    diagnose: { cases: d.length, unusable: unusable('diagnose'), correct: d.filter(x => x.correct).length, abstained: d.filter(x => x.disposition === 'unresolved').length, errors: d.filter(x => x.status === 'error').length, falseGreens: d.filter(x => x.falseGreen).length, confusion, ...timing(d), usage: usage(d) },
    repair: { cases: r.length, unusable: unusable('repair'), verified: verified.length, verifiedCorrect: r.filter(x => x.verifiedCorrect).length, precision: verified.length ? r.filter(x => x.verifiedCorrect).length / verified.length : null, recovery: repairable.length ? repairable.filter(x => x.verifiedCorrect).length / repairable.length : null, falseGreens: r.filter(x => x.falseGreen).length, ...timing(r), usage: usage(r) },
    goal: { cases: g.length, unusable: unusable('goal'), skipped: rows.filter(x => x.lane === 'goal' && x.skipped).length, goalStatusMatched: g.filter(x => x.goalCorrect).length, testVerdictMatched: g.filter(x => x.testCorrect).length, completedButFailed: g.filter(x => x.goal === 'completed' && x.test !== 'passed').length, falseGreens: g.filter(x => x.falseGreen).length, ...timing(g), usage: usage(g) },
  };
}

function campaignJevUsage(rows) {
  const totals = { requests: 0, requestsWithUsage: 0, inputTokens: 0, outputTokens: 0, caseRunsWithoutUsageReport: 0 };
  for (const { jevUsage: usage } of rows) {
    if (!usage) {
      totals.caseRunsWithoutUsageReport++;
      continue;
    }
    totals.requests += usage.requests;
    totals.requestsWithUsage += usage.requestsWithUsage;
    totals.inputTokens += usage.inputTokens;
    totals.outputTokens += usage.outputTokens;
    const estimate = usage.inputRateEstimate;
    if (!estimate) continue;
    if (!totals.inputRateEstimate) {
      totals.inputRateEstimate = { ...estimate, comparisons: estimate.comparisons.map(c => ({ ...c })) };
      continue;
    }
    const aggregate = totals.inputRateEstimate;
    aggregate.estimatedCostNanoUSD += estimate.estimatedCostNanoUSD;
    // Every case runs the same binary, so rates and model order match.
    for (const [index, c] of estimate.comparisons.entries()) {
      aggregate.comparisons[index].estimatedCostNanoUSD += c.estimatedCostNanoUSD;
      aggregate.comparisons[index].estimatedSavingsNanoUSD += c.estimatedSavingsNanoUSD;
    }
  }
  return totals;
}

function report(meta, rows, summary) {
  const pct = v => v == null ? 'n/a' : `${Math.round(v * 100)}%`;
  const lines = [`# Jev qualification — ${meta.mode}`, '', `- Date: ${meta.date}`, `- Revision: ${meta.revision}`, `- Corpus: ${meta.corpus} (held-out ${meta.heldout.slice(0, 12)})`, `- Partition: ${meta.partition} · repeats: ${meta.repeat} · images: ${meta.images}`, `- Model: ${meta.models.join(', ') || (meta.mode === 'jev' ? 'not recorded' : 'none (no provider calls)')}`, ''];
  if (meta.mode !== 'jev') lines.push('**Policy/wiring evidence only.** No provider request was made; these results are not Jev quality measurements.', '');
  if (meta.mode === 'validate') {
    lines.push('| case | labels hold | evidence |', '|---|---|---|', ...rows.map(r => `| ${r.id} | ${r.ok ? 'yes' : '**no**'} | ${r.detail} |`));
    return lines.join('\n') + '\n';
  }
  const s = summary;
  if (s.jevUsage) {
    const u = s.jevUsage;
    const usd = nano => `$${(nano / 1e9).toFixed(9)}`;
    lines.push('## Jev usage and estimated savings', '', `${u.requestsWithUsage} of ${u.requests} recorded requests reported tokens · ${u.caseRunsWithoutUsageReport} case runs had no usage report.`, '');
    if (u.inputRateEstimate) {
      lines.push(`${u.inputTokens} input and ${u.outputTokens} output tokens reported. Estimated Jev cost ${usd(u.inputRateEstimate.estimatedCostNanoUSD)} USD. Savings apply standard uncached input rates to the same reported token count.`, '',
        ...u.inputRateEstimate.comparisons.map(c => `- ${c.model} · ${usd(c.estimatedSavingsNanoUSD)} USD estimated savings`), '');
    }
  }
  lines.push('## Diagnosis', '', `${s.diagnose.correct}/${s.diagnose.cases} expected dispositions · ${s.diagnose.abstained} unresolved · ${s.diagnose.errors} errors · **${s.diagnose.falseGreens} false greens** · **${s.diagnose.unusable} unusable** · run p50/p95 ${s.diagnose.p50Ms}/${s.diagnose.p95Ms} ms · Jev p50/p95 ${s.diagnose.jevP50Ms ?? 'n/a'}/${s.diagnose.jevP95Ms ?? 'n/a'} ms`, '',
    '| case | expected | disposition | family | conf. | status/reason |', '|---|---|---|---|---|---|',
    ...rows.filter(r => r.lane === 'diagnose').map(r => `| ${r.id} | ${r.expected} | ${r.correct ? '' : '**'}${r.disposition}${r.correct ? '' : '**'} | ${r.family || ''} | ${r.confidence == null ? '' : r.confidence.toFixed(2)} | ${r.status}: ${r.reason} |`), '',
    '## Repair', '', `${s.repair.verified} verified (${s.repair.verifiedCorrect} correct) · precision ${pct(s.repair.precision)} · recovery ${pct(s.repair.recovery)} · **${s.repair.falseGreens} false greens** · **${s.repair.unusable} unusable** · run p50/p95 ${s.repair.p50Ms}/${s.repair.p95Ms} ms`, '',
    '| case | expected | state | stage | chosen | decision |', '|---|---|---|---|---|---|',
    ...rows.filter(r => r.lane === 'repair').map(r => `| ${r.id} | ${r.expected}${r.expectedName ? ` (${r.expectedName})` : ''} | ${r.state} | ${r.stage} | ${r.chosen || ''} | ${r.decision ? `${r.decision}: ${r.decisionReason}` : ''} |`), '',
    '## Goals', '', s.goal.cases ? `${s.goal.goalStatusMatched}/${s.goal.cases} goal statuses as labeled · ${s.goal.testVerdictMatched}/${s.goal.cases} test verdicts as labeled · ${s.goal.completedButFailed} completed-but-failed · **${s.goal.falseGreens} false greens** · **${s.goal.unusable} unusable** · run p50/p95 ${s.goal.p50Ms}/${s.goal.p95Ms} ms` : `Not run: ${s.goal.skipped} cases need a goal provider.`, '',
    '| case | expected goal / test | goal | test | actions |', '|---|---|---|---|---|',
    ...rows.filter(r => r.lane === 'goal').map(r => r.skipped ? `| ${r.id} | | skipped | | ${r.skipped} |` : `| ${r.id} | ${r.expectedGoal.join(' or ')} / ${r.expectedTest} | ${r.goal} | ${r.test} | ${r.unusable ? `unusable: ${r.unusable}` : r.actions} |`), '',
    'A diagnosis never changes a result; goal completion is never a pass. Unusable rows (a CLI error, a run that did not complete or missing evidence) fail the qualification and are not scored. False greens count any changed failed result, an assertion failure marked eligible for repair, a verified wrong or unlabeled repair, a modified source, or a passing test where the label requires a failure.');
  return lines.join('\n') + '\n';
}

function main() {
  const options = parseArgs(process.argv.slice(2));
  const manifest = loadManifest();
  const cases = manifest.cases.filter(c => options.lanes.includes(c.lane) && (options.partition === 'all' || c.partition === options.partition));
  if (options.mode === 'jev') {
    const planned = plannedRequests(cases, options);
    if (!process.env.TYPESAFE_API_KEY || !options.confirmPaid || options.maxRequests < planned) {
      console.error(`Live qualification makes paid Jev requests: at most ${planned} for ${cases.length} cases × ${options.repeat}. Re-run with TYPESAFE_API_KEY in this process, --confirm-paid and --max-requests ${planned}. No request was made.`);
      process.exitCode = 2;
      return;
    }
  }
  const stamp = new Date().toISOString().replace(/[-:]/g, '').replace(/\..*/, 'Z');
  const out = path.join(root, '.context', 'jev-qualification', `${stamp}-${options.mode}`);
  fs.mkdirSync(out, { recursive: true });
  const binary = path.join(out, process.platform === 'win32' ? '9l.exe' : '9l');
  const build = spawnSync('go', ['build', '-o', binary, './cmd/9l'], { cwd: root, encoding: 'utf8' });
  if (build.status !== 0) throw new Error('go build failed: ' + build.stderr);
  const revision = spawnSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).stdout.trim();
  const receipts = path.join(out, 'receipts');
  const rows = [];
  for (let repeat = 0; repeat < options.repeat; repeat++) {
    for (const c of cases) {
      let row;
      if (options.mode === 'validate') row = validateCase(binary, c, receipts);
      else row = ({ diagnose: diagnoseCase, repair: repairCase, goal: goalCase })[c.lane](binary, c, options, receipts);
      row.repeat = repeat + 1;
      rows.push(row);
      const flag = options.mode === 'validate' ? (row.ok ? 'ok' : 'LABEL FAILS') : row.skipped ? 'skipped' : row.unusable ? `UNUSABLE: ${row.unusable}` : row.falseGreen ? 'FALSE GREEN' : 'ok';
      console.log(`${c.id.padEnd(34)} ${flag}`);
    }
  }
  const summary = options.mode === 'validate' ? null : summarize(rows);
  if (options.mode === 'jev') summary.jevUsage = campaignJevUsage(rows);
  const models = [...new Set(rows.flatMap(r => (r.model || '').split(', ')).filter(Boolean))];
  const meta = { version: '9l.jev-qualification/1', mode: options.mode, date: new Date().toISOString(), revision, corpus: manifest.version, heldout: manifest.heldoutSHA256, partition: options.partition, repeat: options.repeat, images: options.images, models, platform: `${process.platform}/${process.arch}` };
  fs.writeFileSync(path.join(out, 'results.json'), JSON.stringify({ meta, summary, rows }, null, 2) + '\n');
  fs.writeFileSync(path.join(out, 'report.md'), report(meta, rows, summary));
  console.log(`Report: ${path.relative(root, path.join(out, 'report.md'))}`);
  const failed = options.mode === 'validate' ? rows.some(r => !r.ok) : rows.some(r => r.falseGreen || r.unusable);
  if (failed) process.exitCode = 1;
}

module.exports = { loadManifest, plannedRequests, sortKeys, renamed, summarize, percentile, parseArgs, goalScript, goalRow, campaignJevUsage };
if (require.main === module) {
  try { main(); } catch (error) { console.error(error.message); process.exitCode = 2; }
}
