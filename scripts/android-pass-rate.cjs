#!/usr/bin/env node
// Summarizes repeated real-emulator Android runs as a pass rate per case.
//
//   node scripts/android-pass-rate.cjs <directory> [--expect <case...>]
//
// <directory> holds the downloaded `android-qualification-*` artifacts. Every
// `results.json` below it is one `scripts/android-smoke.cjs` run: a case
// qualified unless its record says `qualified: false`. The Markdown table goes
// to stdout (CI appends it to the job summary); a case named with --expect
// that has no record is listed as not run. The exit code is always 0: the
// emulator jobs already fail on an unqualified case, and this only measures.
'use strict';
const fs = require('node:fs');
const path = require('node:path');

function resultFiles(directory) {
  if (!fs.existsSync(directory)) return [];
  return fs.readdirSync(directory, {withFileTypes: true}).flatMap(entry => {
    const full = path.join(directory, entry.name);
    if (entry.isDirectory()) return resultFiles(full);
    return entry.name === 'results.json' ? [full] : [];
  }).sort();
}

// Only the case name, the qualification and the smoke's own one-line reason
// are read; probe facts stay in the artifacts.
function passRates(files, expected = []) {
  const cases = new Map(expected.map(mode => [mode, {runs: 0, qualified: 0, reasons: new Map()}]));
  let unreadable = 0;
  for (const file of files) {
    let parsed;
    try { parsed = JSON.parse(fs.readFileSync(file, 'utf8')); } catch { unreadable++; continue; }
    if (!parsed || parsed.version !== 1 || !Array.isArray(parsed.cases)) { unreadable++; continue; }
    for (const record of parsed.cases) {
      if (!record || typeof record.mode !== 'string' || !/^[a-z0-9-]{1,64}$/.test(record.mode)) continue;
      const entry = cases.get(record.mode) ?? {runs: 0, qualified: 0, reasons: new Map()};
      entry.runs++;
      if (record.qualified === false) {
        const reason = typeof record.reason === 'string' ? record.reason.split('\n')[0].slice(0, 120) : 'unknown';
        entry.reasons.set(reason, (entry.reasons.get(reason) ?? 0) + 1);
      } else {
        entry.qualified++;
      }
      cases.set(record.mode, entry);
    }
  }
  return {cases, files: files.length, unreadable};
}

function markdown({cases, files, unreadable}) {
  const cell = text => String(text).replace(/[|\r\n]/g, ' ');
  const rows = [...cases.entries()].sort(([a, x], [b, y]) => (x.runs ? x.qualified / x.runs : -1) - (y.runs ? y.qualified / y.runs : -1) || a.localeCompare(b));
  const lines = [
    '## Android pass rate',
    '',
    `${files} smoke run(s) read${unreadable ? `, ${unreadable} unreadable` : ''}. A case qualifies when it ends exactly as the smoke expects.`,
    '',
    '| Case | Qualified | Rate | Reasons it did not qualify |',
    '| --- | --- | --- | --- |',
  ];
  for (const [mode, entry] of rows) {
    const rate = entry.runs ? `${Math.round(100 * entry.qualified / entry.runs)}%` : 'not run';
    const reasons = [...entry.reasons.entries()].map(([reason, count]) => `${count}× ${cell(reason)}`).join('; ');
    lines.push(`| ${cell(mode)} | ${entry.qualified}/${entry.runs} | ${rate} | ${reasons} |`);
  }
  return lines.join('\n') + '\n';
}

if (require.main === module) {
  const [directory, flag, ...expected] = process.argv.slice(2);
  if (!directory || (flag !== undefined && flag !== '--expect')) {
    console.error('usage: node scripts/android-pass-rate.cjs <directory> [--expect <case...>]');
    process.exit(2);
  }
  process.stdout.write(markdown(passRates(resultFiles(directory), expected)));
}

module.exports = {resultFiles, passRates, markdown};
