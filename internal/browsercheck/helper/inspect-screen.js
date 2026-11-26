"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.shortLabel = exports.maxControls = void 0;
exports.testabilityOf = testabilityOf;
exports.rankControls = rankControls;
exports.countTestIds = countTestIds;
exports.renderScreenInspectReport = renderScreenInspectReport;
// The platform-neutral half of `9l inspect --android` and `--ios`: each
// control's handle, the testability score and the Markdown report. The
// platform modules decide which handle the 9l driver would use.
const report_1 = require("./report");
exports.maxControls = 300;
const maxLabel = 80;
/** One line, bounded: a merged multi-line label keeps its breaks as ⏎. */
const shortLabel = (value) => {
    const flat = value.replace(/\s*\n\s*/g, ' ⏎ ').replace(/\s+/g, ' ').trim();
    return flat.length > maxLabel ? `${flat.slice(0, maxLabel - 1)}…` : flat;
};
exports.shortLabel = shortLabel;
function testabilityOf(controls, copyMeans) {
    const tally = (value) => controls.filter((control) => control.stability === value).length;
    const testability = {
        controls: controls.length, stable: tally('stable'), repeated: tally('repeated'), copy: tally('copy'), none: tally('none'),
        score: controls.length === 0 ? 100 : Math.round((tally('stable') / controls.length) * 100),
    };
    const missing = controls.length - testability.stable;
    if (missing > 0) {
        testability.note = `${missing} of ${controls.length} controls have no unique test ID: ` +
            [testability.repeated && `${testability.repeated} share an ID`, testability.copy && `${testability.copy} ${copyMeans}`,
                testability.none && `${testability.none} only by coordinates`].filter(Boolean).join(', ') + '.';
    }
    return testability;
}
/** Weakest handle first, screen order within a handle, at most maxControls. */
function rankControls(controls) {
    const order = { none: 0, copy: 1, repeated: 2, stable: 3 };
    const ranked = controls.map((control, index) => ({ control, index }))
        .sort((a, b) => order[a.control.stability] - order[b.control.stability] || a.index - b.index).map(({ control }) => control);
    const shown = ranked.slice(0, exports.maxControls);
    return { controls: shown, ...(ranked.length > shown.length ? { truncated: ranked.length - shown.length } : {}) };
}
const maxTestIds = 2000;
/** Count the test IDs a window's elements carry, bounded. */
function countTestIds(ids) {
    const counts = new Map();
    let truncated = false;
    for (const id of ids) {
        if (!id)
            continue;
        if (!counts.has(id) && counts.size >= maxTestIds) {
            truncated = true;
            continue;
        }
        counts.set(id, (counts.get(id) ?? 0) + 1);
    }
    const testIds = Object.fromEntries([...counts].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)));
    return { testIds, ...(truncated ? { testIdsTruncated: true } : {}) };
}
const copyBadge = { android: '🟠 copy only', ios: '🟠 label only' };
const footer = {
    android: 'one read-only uiautomator dump of the active window',
    ios: 'one WebDriverAgent page source of the foreground app',
};
function renderScreenInspectReport(result, opts = {}) {
    const date = (opts.now ?? new Date()).toISOString().slice(0, 10);
    const t = result.testability;
    const badge = { stable: '🟢 stable', repeated: '🟡 repeated ID', copy: copyBadge[result.platform], none: '🔴 none' };
    const platform = result.platform === 'ios' ? 'iOS' : 'Android';
    const lines = [`# Screen Inspection — ${platform}${result.apps.length ? ` · ${result.apps.join(', ')}` : ''}`, ''];
    for (const warning of result.warnings)
        lines.push(`> ⚠️ ${warning}`, '');
    const shares = [t.stable && `${t.stable} with a unique test ID`, t.repeated && `${t.repeated} with a shared ID`,
        t.copy && `${t.copy} by ${result.platform === 'ios' ? 'label' : 'copy'} only`, t.none && `${t.none} without a handle`].filter(Boolean);
    lines.push(`**Testability: ${t.score} / 100**   ·   ${t.controls} ${t.controls === 1 ? 'control' : 'controls'}${shares.length ? `: ${shares.join(' · ')}` : ''}   ·   inspected ${date}`, '');
    lines.push(`\`${(0, report_1.meter)(t.score, 100, 24)}\` ${t.score} / 100`, '');
    if (t.note)
        lines.push(`_${t.note}_`, '');
    if (result.controls.length) {
        lines.push('## Controls, weakest handle first', '', '| Control | Class | Handle | 9l locator |', '|---------|-------|:------:|------------|');
        for (const control of result.controls) {
            const name = control.label || `(unlabelled, ${control.bounds})`;
            const className = control.className.replace(/^android\.(widget|view)\./, '').replace(/^XCUIElementType/, '');
            lines.push(`| ${(0, report_1.cell)(name)} | ${(0, report_1.cell)(className)} | ${badge[control.stability]} | ${control.locator ? (0, report_1.codeSpan)((0, report_1.cell)(control.locator)) : '—'} |`);
        }
        if (result.truncated)
            lines.push('', `…and ${result.truncated} more controls with stronger handles.`);
        lines.push('');
        const notes = new Map();
        for (const control of result.controls) {
            if (!control.note)
                continue;
            notes.set(control.note, [...(notes.get(control.note) ?? []), control.label || `(unlabelled, ${control.bounds})`]);
        }
        if (notes.size) {
            lines.push('### What to fix', '');
            for (const [note, labels] of notes) {
                lines.push(`- **${(0, report_1.cell)(labels.slice(0, 5).join(', '))}${labels.length > 5 ? ` and ${labels.length - 5} more` : ''}** — ${note}`);
            }
            lines.push('');
        }
        if (result.platform === 'android') {
            lines.push('A copy-only control is used through the Android driver\'s `controls` option, for example `controls: {save: {text: "Save"}}` and then `app.getByTestId("save")`.', '');
        }
    }
    lines.push('---', '', `_Inspected by **9lives** · \`9l inspect --${result.platform}\` · ${footer[result.platform]} · local, no account_`, '');
    return lines.join('\n');
}
