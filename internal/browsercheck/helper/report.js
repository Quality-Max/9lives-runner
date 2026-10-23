"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.renderIssues = renderIssues;
exports.renderReport = renderReport;
exports.renderInspectReport = renderInspectReport;
const common_1 = require("./common");
const signals_1 = require("./checks/signals");
const SEVERITY_EMOJI = {
    critical: '🔴',
    high: '🔴',
    medium: '🟠',
    low: '🟡',
    info: '🔵',
};
const GRADE_EMOJI = {
    A: '🟢',
    B: '🟢',
    C: '🟡',
    D: '🟠',
    F: '🔴',
};
const CHECK_META = [
    { check: 'console', label: 'Console errors', categories: ['console', 'network', 'telemetry'] },
    { check: 'links', label: 'Broken links', categories: ['links'] },
    { check: 'accessibility', label: 'Accessibility', categories: ['accessibility'] },
    { check: 'performance', label: 'Performance', categories: ['performance'] },
    { check: 'seo', label: 'SEO', categories: ['seo'] },
    { check: 'security_headers', label: 'Security headers', categories: ['security_headers'] },
    { check: 'cookies', label: 'Cookies and trackers', categories: ['cookies'] },
    { check: 'mixed_content', label: 'Mixed content', categories: ['mixed_content'] },
    { check: 'weight', label: 'Page weight', categories: ['weight'] },
];
/**
 * A text meter. Wrapped in a code span by callers so Markdown renders it in a monospace font and
 * the bars stay aligned down the column.
 */
function meter(value, max, width) {
    if (max <= 0)
        return '░'.repeat(width);
    const filled = Math.max(0, Math.min(width, Math.round((value / max) * width)));
    return '█'.repeat(filled) + '░'.repeat(width - filled);
}
/** The score rule in one line, so the number above it can be checked by hand. */
function scoringLine(scoring) {
    const weights = ['critical', 'high', 'medium', 'low', 'info']
        .map((severity) => `${scoring.weights[severity]} per ${severity}`)
        .join(', ');
    const grades = ['A', 'B', 'C', 'D'].map((grade) => `${grade} ≥ ${scoring.grades[grade]}`).join(', ');
    return `Score: ${scoring.scale.max} minus ${weights} finding, floored at ${scoring.scale.min}. Grades: ${grades}, else F.`;
}
/** A finding's URL lines: the one URL it has, or the list a grouped finding carries. */
function urlLines(finding) {
    if (finding.url)
        return [`\`${finding.url}\``];
    if (!finding.urls?.length)
        return [];
    const lines = finding.urls.map((url) => `- \`${url}\``);
    if (finding.omittedUrls)
        lines.push(`- …and ${finding.omittedUrls} more`);
    return lines;
}
function host(url) {
    try {
        return new URL(url).host;
    }
    catch {
        return url;
    }
}
function worstSeverity(findings) {
    if (findings.length === 0)
        return null;
    return findings.reduce((worst, f) => (common_1.SEVERITY_RANK[f.severity] < common_1.SEVERITY_RANK[worst] ? f.severity : worst), 'info');
}
function reproBlock(repro) {
    // Multi-line repros are DevTools step lists; one-liners are shell commands.
    if (repro.includes('\n')) {
        return `**Reproduce:**\n\n${repro}\n`;
    }
    return `**Reproduce:**\n\n\`\`\`bash\n${repro}\n\`\`\`\n`;
}
/** Render measured values that are context rather than defects. */
function measurementLines(metrics) {
    if (!metrics?.vitals && !metrics?.weight)
        return [];
    const rows = [];
    const vitals = metrics.vitals;
    if (vitals) {
        const ms = (value) => (value === null ? 'not available' : `${value}ms`);
        rows.push(`| Largest Contentful Paint | ${ms(vitals.lcpMs)} |`);
        rows.push(`| Cumulative Layout Shift | ${vitals.clsScore === null ? 'not available' : vitals.clsScore.toFixed(3)} |`);
        rows.push(`| Time to First Byte | ${ms(vitals.ttfbMs)} |`);
        rows.push(`| First Contentful Paint | ${ms(vitals.fcpMs)} |`);
        rows.push('| Interaction to Next Paint | not measured — needs real user interaction |');
    }
    const weight = metrics.weight;
    if (weight) {
        const qualifier = weight.totalBytesComplete ? '' : ' (lower bound)';
        rows.push(`| Page transfer | ${(0, signals_1.formatBytes)(weight.totalBytes)}${qualifier} across ${weight.requestCount} requests |`);
        const thirdParty = weight.thirdPartyOrigins.filter((origin) => origin.bytes > 0).slice(0, 3);
        if (thirdParty.length > 0) {
            const summary = thirdParty.map((origin) => `${origin.host} ${(0, signals_1.formatBytes)(origin.bytes)}`).join(', ');
            rows.push(`| Heaviest third parties | ${summary} |`);
        }
        const slowest = weight.slowestRequests[0];
        if (slowest) {
            rows.push(`| Slowest request | ${slowest.durationMs}ms — ${slowest.url} |`);
        }
    }
    const lines = ['---', '', '## Measurements', '', '| Metric | Value |', '|--------|-------|', ...rows, ''];
    // Byte breakdown as a bar list: which resource types actually account for the page's weight.
    const byType = Object.entries(weight?.bytesByResourceType ?? {})
        .filter(([, bytes]) => bytes > 0)
        .sort((a, b) => b[1] - a[1])
        .slice(0, 6);
    if (byType.length > 0) {
        const widestType = Math.max(...byType.map(([type]) => type.length));
        const widestSize = Math.max(...byType.map(([, bytes]) => (0, signals_1.formatBytes)(bytes).length));
        lines.push('Where the bytes went:');
        lines.push('');
        lines.push('```');
        for (const [type, bytes] of byType) {
            lines.push(`${type.padEnd(widestType)}  ${meter(bytes, byType[0][1], 16)}  ${(0, signals_1.formatBytes)(bytes).padStart(widestSize)}`);
        }
        lines.push('```');
        lines.push('');
    }
    for (const note of vitals?.notes ?? []) {
        lines.push(`_${note}_`);
        lines.push('');
    }
    return lines;
}
/**
 * Render each finding as a self-contained, tracker-agnostic issue block.
 *
 * Findings already carry roughly what a ticket needs — message, repro,
 * suggestion, selector or URL, severity — but as report fields rather than
 * ticket fields, so filing one means rewriting all of it as prose. In the
 * session that motivated this, six findings became six hand-written tickets and
 * each needed the same transformation. Teams that must do that by hand file the
 * top one or two findings and drop the rest, which is where findings die.
 *
 * The blocks are separated by HTML comments rather than headings: a comment
 * renders as nothing in every tracker, so a block can be pasted straight into a
 * description without carrying the report's own structure with it.
 */
function renderIssues(result, opts = {}) {
    const date = (opts.now ?? new Date()).toISOString().slice(0, 10);
    const floor = opts.minSeverity ? common_1.SEVERITY_RANK[opts.minSeverity] : common_1.SEVERITY_RANK.info;
    const selected = [...result.findings]
        .filter((finding) => common_1.SEVERITY_RANK[finding.severity] <= floor)
        .sort((a, b) => common_1.SEVERITY_RANK[a.severity] - common_1.SEVERITY_RANK[b.severity] || a.category.localeCompare(b.category));
    if (selected.length === 0) {
        const qualifier = opts.minSeverity ? ` at or above ${opts.minSeverity}` : '';
        return `<!-- 9lives: no findings${qualifier} for ${result.url} on ${date} -->\n`;
    }
    const blocks = selected.map((finding, index) => {
        // Where the problem is: a selector when the finding has one, the URL when it
        // does not, and nothing rather than a guess when it has neither.
        const locus = finding.selector
            ? `\`${finding.selector}\``
            : finding.url
                ? finding.url
                : finding.urls?.length
                    ? finding.urls.join(', ') + (finding.omittedUrls ? ` and ${finding.omittedUrls} more` : '')
                    : undefined;
        const lines = [];
        lines.push(`<!-- issue ${index + 1} of ${selected.length} · ${finding.severity} · ${finding.category} -->`);
        lines.push('');
        lines.push('## Summary');
        lines.push(locus ? `${finding.message} (${locus})` : finding.message);
        lines.push('');
        lines.push('## Steps to Reproduce');
        if (finding.repro) {
            // A multi-line repro is already a numbered list; a one-liner is a command.
            lines.push(finding.repro.includes('\n') ? finding.repro : `1. Run \`${finding.repro}\``);
        }
        else {
            lines.push(`1. Open ${result.url}`);
            if (locus)
                lines.push(`2. Inspect ${locus}`);
        }
        lines.push('');
        lines.push('## Expected Result');
        // The suggestion states the fix, which is the same information as the
        // expected end state. Without one there is nothing honest to assert beyond
        // the absence of the finding.
        lines.push(finding.suggestion ?? 'The scan reports no finding here.');
        lines.push('');
        lines.push('## Actual Result');
        lines.push(finding.occurrences && finding.occurrences > 1
            ? `${finding.message} Observed ${finding.occurrences} times on one page load.`
            : finding.message);
        lines.push('');
        lines.push('## Environment');
        lines.push(`Found by an automated scan (9lives \`scan_url\`, Chromium) against ${result.url} on ${date}. ` +
            `Severity ${finding.severity}, category ${finding.category}.`);
        return lines.join('\n');
    });
    return `${blocks.join('\n\n---\n\n')}\n`;
}
/** Render a scan result as a shareable Markdown report. */
function renderReport(result, opts = {}) {
    const scoring = result.scoring ?? (0, common_1.describeScoring)(result.score);
    const grade = scoring.grade;
    const ranThese = new Set(result.checks);
    const date = (opts.now ?? new Date()).toISOString().slice(0, 10);
    const lines = [];
    lines.push(`# QA Scan — ${host(result.url)}`);
    lines.push('');
    lines.push(`**Grade: ${GRADE_EMOJI[grade]} ${grade}  (${result.score} / 100)**   ·   ` +
        `${result.findingCount} ${result.findingCount === 1 ? 'issue' : 'issues'} found   ·   scanned ${date}`);
    lines.push('');
    lines.push(`\`${meter(result.score, 100, 24)}\` ${result.score} / 100`);
    lines.push('');
    lines.push(`_${scoringLine(scoring)}_`);
    lines.push('');
    // The verdict goes above the tables: when a baseline was supplied, "did this
    // change introduce anything?" is the question the reader came with.
    if (result.delta) {
        lines.push(`**Since baseline:** ${result.delta.verdict}`);
        lines.push('');
        const bullets = [
            ['New', result.delta.new],
            ['Fixed', result.delta.fixed],
        ];
        for (const [label, items] of bullets) {
            for (const item of items.slice(0, 10)) {
                lines.push(`- ${label}: ${item.message}${item.url ? ` (\`${item.url}\`)` : ''}`);
            }
            if (items.length > 10)
                lines.push(`- ${label}: …and ${items.length - 10} more`);
        }
        if (result.delta.new.length > 0 || result.delta.fixed.length > 0)
            lines.push('');
    }
    // Category summary table — one row per check that actually ran, with a bar scaled to the noisiest
    // category so the shape of the result is readable before any row is.
    const counts = CHECK_META.filter((meta) => ranThese.has(meta.check)).map((meta) => ({
        meta,
        matched: result.findings.filter((f) => meta.categories.includes(f.category)),
    }));
    const busiest = Math.max(0, ...counts.map((entry) => entry.matched.length));
    const rows = counts.map(({ meta, matched }) => {
        const worst = worstSeverity(matched);
        const bar = busiest > 0 ? ` \`${meter(matched.length, busiest, 10)}\`` : '';
        return `| ${meta.label} |${bar} ${matched.length} | ${worst ? `${SEVERITY_EMOJI[worst]} ${worst}` : '—'} |`;
    });
    if (rows.length) {
        lines.push('| Category | Issues | Worst |');
        lines.push('|----------|--------|:-----:|');
        lines.push(...rows);
        lines.push('');
    }
    lines.push(...measurementLines(result.metrics));
    if (result.findings.length === 0) {
        lines.push('---');
        lines.push('');
        lines.push('✅ No issues found in the checks that ran.');
    }
    else {
        const sorted = [...result.findings].sort((a, b) => common_1.SEVERITY_RANK[a.severity] - common_1.SEVERITY_RANK[b.severity] || a.category.localeCompare(b.category));
        for (const f of sorted) {
            lines.push('---');
            lines.push('');
            const times = f.occurrences && f.occurrences > 1 ? ` (seen ${f.occurrences} times)` : '';
            lines.push(`## ${SEVERITY_EMOJI[f.severity]} ${f.severity} · ${f.message}${times}`);
            lines.push(...urlLines(f));
            if (f.selector)
                lines.push(`Selector: \`${f.selector}\``);
            lines.push('');
            if (f.repro) {
                lines.push(reproBlock(f.repro));
            }
            if (f.suggestion) {
                lines.push(`**Fix:** ${f.suggestion}`);
                lines.push('');
            }
        }
    }
    if (result.screenshotPath) {
        lines.push(`Screenshot: \`${result.screenshotPath}\``);
        lines.push('');
    }
    lines.push('---');
    lines.push('');
    lines.push('_Scanned by **9lives** · `9l scan <url>` · local, no account_');
    lines.push('');
    return lines.join('\n');
}
const STABILITY_EMOJI = {
    stable: '🟢',
    acceptable: '🟡',
    fragile: '🟠',
    none: '🔴',
};
const STABILITY_RANK = { stable: 0, acceptable: 1, fragile: 2, none: 3 };
/** Escape a value for a Markdown table cell without breaking its code span. */
function cell(value) {
    return value.replaceAll('|', '\\|').replaceAll('\n', ' ');
}
/**
 * Wrap a page-derived value in a Markdown code span it cannot break out of.
 *
 * Backslash escapes do not work inside code spans, so a backtick in the value
 * is handled the way CommonMark intends: the delimiter is one backtick longer
 * than the longest backtick run inside, and a value that starts or ends with a
 * backtick is padded so the delimiter stays unambiguous. The content is
 * preserved exactly — a selector stays copy-pasteable.
 */
function codeSpan(value) {
    const runs = value.match(/`+/g);
    const delimiter = '`'.repeat((runs ? Math.max(...runs.map((run) => run.length)) : 0) + 1);
    const padded = value.startsWith('`') || value.endsWith('`') ? ` ${value} ` : value;
    return `${delimiter}${padded}${delimiter}`;
}
/**
 * A short human label for a control: what it is, then what it is called.
 *
 * Only an accessible name goes in quotes. A name read off the `name` attribute
 * used to be quoted exactly like a label, so an unlabelled checkbox read as
 * named in the same report where `scan` flagged it as having no label.
 */
function controlLabel(control) {
    const kind = control.role || (control.type ? `${control.tag}[${control.type}]` : control.tag);
    const accessibleName = control.accessibleName ?? (control.nameSource === 'name-attribute' ? '' : (control.name ?? ''));
    if (accessibleName)
        return `${kind} “${accessibleName}”`;
    const handle = control.nameSource === 'name-attribute' && control.name
        ? `[name=${control.name}]`
        : control.selector
            ? codeSpan(control.selector)
            : '';
    const label = handle ? `${kind} ${handle}` : kind;
    // Only a role that needs a name is missing one: an unnamed list, group or
    // test-id wrapper is not a defect, and saying so would read as one.
    return control.type?.toLowerCase() !== 'hidden' &&
        control.role &&
        common_1.ROLES_REQUIRING_A_NAME.has(control.role)
        ? `${label} (no accessible name)`
        : label;
}
/**
 * Render an `inspect_page` result as a shareable Markdown report.
 *
 * The scan renderer leads with the grade because "how bad is it?" is the scan
 * question. Here the question is "what do I write in my spec?", so the locator
 * table — best handle first, ready to paste — is the body of the report, and
 * the testability verdict above it says how far those locators can be trusted
 * before the first one is read.
 */
function renderInspectReport(result, opts = {}) {
    const date = (opts.now ?? new Date()).toISOString().slice(0, 10);
    const t = result.testability;
    const lines = [];
    lines.push(`# Page Inspection — ${host(result.url)}`);
    lines.push('');
    if (result.title) {
        // The one page-derived field the snapshot does not whitespace-collapse;
        // without this, a newline in document.title injects report structure.
        lines.push(`**${result.title.replace(/\s+/g, ' ').trim()}**`);
        lines.push('');
    }
    // Warnings go before any numbers they would undermine: a testability score
    // over an empty snapshot is not a verdict on the page.
    for (const warning of result.warnings ?? []) {
        lines.push(`> ⚠️ ${warning}`);
        lines.push('');
    }
    const shares = ['stable', 'acceptable', 'fragile']
        .map((key) => (t[key] > 0 ? `${t[key]} ${key}` : null))
        .filter((part) => part !== null);
    if (t.none > 0)
        shares.push(`${t.none} without a handle`);
    lines.push(`**Testability: ${t.score} / 100**   ·   ${t.controls} ${t.controls === 1 ? 'control' : 'controls'}` +
        `${shares.length > 0 ? `: ${shares.join(' · ')}` : ''}   ·   inspected ${date}`);
    lines.push('');
    lines.push(`\`${meter(t.score, 100, 24)}\` ${t.score} / 100`);
    lines.push('');
    if (t.note) {
        lines.push(`_${t.note}_`);
        lines.push('');
    }
    if (result.interactive.length > 0) {
        lines.push('## Locators, best handle first');
        lines.push('');
        lines.push('| Control | Stability | Locator |');
        lines.push('|---------|:---------:|---------|');
        const ranked = [...result.interactive].sort((a, b) => (STABILITY_RANK[a.stability ?? 'none'] ?? 3) - (STABILITY_RANK[b.stability ?? 'none'] ?? 3));
        for (const control of ranked) {
            const stability = control.stability ?? 'none';
            const badge = `${STABILITY_EMOJI[stability] ?? '🔴'} ${stability}`;
            const locator = control.recommendedLocator ? codeSpan(cell(control.recommendedLocator)) : '—';
            lines.push(`| ${cell(controlLabel(control))} | ${badge} | ${locator} |`);
        }
        lines.push('');
        // One caveat can apply to thirty controls — an unlabelled input rarely
        // travels alone — so caveats group by their text instead of repeating it.
        const caveats = new Map();
        for (const control of ranked) {
            if (!control.locatorNote)
                continue;
            const labels = caveats.get(control.locatorNote) ?? [];
            labels.push(controlLabel(control));
            caveats.set(control.locatorNote, labels);
        }
        if (caveats.size > 0) {
            lines.push('### Caveats');
            lines.push('');
            for (const [note, labels] of caveats) {
                const listed = labels.slice(0, 5).join(', ');
                const more = labels.length > 5 ? ` and ${labels.length - 5} more` : '';
                lines.push(`- **${cell(listed)}${more}** — ${note}`);
            }
            lines.push('');
        }
    }
    else {
        lines.push('No interactive controls found.');
        lines.push('');
    }
    if (result.headings.length > 0) {
        lines.push('## Headings');
        lines.push('');
        const shown = result.headings.slice(0, 40);
        for (const heading of shown) {
            lines.push(`${'  '.repeat(Math.max(0, heading.level - 1))}- H${heading.level} ${heading.text}`);
        }
        if (result.headings.length > shown.length) {
            lines.push(`- …and ${result.headings.length - shown.length} more`);
        }
        lines.push('');
    }
    if (result.forms.length > 0) {
        lines.push('## Forms');
        lines.push('');
        for (const form of result.forms) {
            const method = form.method ? form.method.toUpperCase() : 'GET';
            lines.push(`- Form ${form.index}: ${method} ${form.action || '(no action)'} — ${form.fields.length} ${form.fields.length === 1 ? 'field' : 'fields'}`);
        }
        lines.push('');
    }
    lines.push('---');
    lines.push('');
    lines.push('_Inspected by **9lives** · `9l inspect <url>` · local, no account_');
    lines.push('');
    return lines.join('\n');
}
