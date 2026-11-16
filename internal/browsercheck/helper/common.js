"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.MAX_GROUPED_URLS = exports.ROLES_REQUIRING_A_NAME = exports.DEFAULT_VIEWPORT = exports.GRADE_THRESHOLDS = exports.SEVERITY_WEIGHTS = exports.SEVERITY_RANK = void 0;
exports.gradeFromScore = gradeFromScore;
exports.describeScoring = describeScoring;
exports.validateHttpUrl = validateHttpUrl;
exports.loadPlaywright = loadPlaywright;
exports.browserType = browserType;
exports.missingBrowserError = missingBrowserError;
exports.browserContextOptions = browserContextOptions;
exports.withPage = withPage;
exports.resolveWorkspaceStorageStatePath = resolveWorkspaceStorageStatePath;
exports.loadBaselineFindings = loadBaselineFindings;
exports.enforceBrowserNetworkPolicy = enforceBrowserNetworkPolicy;
exports.writeTempFile = writeTempFile;
exports.dedupeFindings = dedupeFindings;
exports.findingFingerprint = findingFingerprint;
exports.withFingerprints = withFingerprints;
exports.diffFindings = diffFindings;
exports.scoreFromFindings = scoreFromFindings;
exports.cssEscape = cssEscape;
const node_crypto_1 = require("node:crypto");
const promises_1 = require("node:fs/promises");
const node_os_1 = require("node:os");
const node_module_1 = require("node:module");
const node_path_1 = __importDefault(require("node:path"));
const doctor_1 = require("./doctor");
const network_policy_1 = require("./network-policy");
const policy_proxy_1 = require("./policy-proxy");
exports.SEVERITY_RANK = {
    critical: 0,
    high: 1,
    medium: 2,
    low: 3,
    info: 4,
};
/** Points each finding costs the score, by severity. Charged once per deduplicated finding. */
exports.SEVERITY_WEIGHTS = {
    critical: 35,
    high: 20,
    medium: 10,
    low: 4,
    info: 0,
};
/** The lowest score each grade starts at. A score below D is an F. */
exports.GRADE_THRESHOLDS = { A: 90, B: 75, C: 60, D: 40, F: 0 };
function gradeFromScore(score) {
    for (const grade of ['A', 'B', 'C', 'D']) {
        if (score >= exports.GRADE_THRESHOLDS[grade])
            return grade;
    }
    return 'F';
}
/**
 * Say how a score was reached, next to the number.
 *
 * A bare `score: 0` on a page with no critical or high finding read as a
 * broken scan in a field report; it was ten medium accessibility findings at
 * ten points each. The scale, the weights and the grade cut-offs appeared
 * nowhere in the output, so checking the number meant finding this file. They
 * travel with the result instead, and the Markdown report prints them.
 */
function describeScoring(score) {
    return {
        scale: { min: 0, max: 100 },
        weights: { ...exports.SEVERITY_WEIGHTS },
        grades: { ...exports.GRADE_THRESHOLDS },
        grade: gradeFromScore(score),
        formula: '100 minus the weight of every finding after deduplication, floored at 0',
    };
}
exports.DEFAULT_VIEWPORT = { width: 1366, height: 900 };
function validateHttpUrl(url) {
    const parsed = new URL(url);
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
        throw new Error('URL must use http or https');
    }
    return parsed.toString();
}
/**
 * Load the project's own Playwright installation.
 *
 * The 9l binary runs these checks from an embedded copy outside the project,
 * so Playwright is resolved from the working directory, where `9l` was started,
 * exactly as a test run uses the project's installation. `@playwright/test`
 * re-exports the browser types for projects that only install the test runner.
 */
function loadPlaywright(root = process.cwd()) {
    // The package's own installation is the fallback for its tests and other
    // callers in this repository.
    for (const load of [(0, node_module_1.createRequire)(node_path_1.default.join(root, 'package.json')), require]) {
        for (const name of ['playwright', '@playwright/test']) {
            let resolved;
            try {
                resolved = load.resolve(name);
            }
            catch {
                continue;
            }
            return load(resolved);
        }
    }
    throw new Error('Playwright is not installed in this project; install @playwright/test and its Chromium browser.');
}
function browserType(name = 'chromium') {
    const playwright = loadPlaywright();
    if (name === 'firefox')
        return playwright.firefox;
    if (name === 'webkit')
        return playwright.webkit;
    return playwright.chromium;
}
// Roles a user operates or must be able to identify. A nameless one of these is
// exactly what an accessibility tree should surface, so it is flagged rather
// than dropped.
exports.ROLES_REQUIRING_A_NAME = new Set([
    'alertdialog',
    'application',
    'button',
    'checkbox',
    'columnheader',
    'combobox',
    'dialog',
    'doc-backlink',
    'doc-biblioentry',
    'doc-biblioref',
    'doc-glossref',
    'doc-noteref',
    'doc-pagebreak',
    'doc-part',
    'graphics-document',
    'graphics-symbol',
    'grid',
    'heading',
    'img',
    'link',
    'listbox',
    'marquee',
    'menuitem',
    'menuitemcheckbox',
    'menuitemradio',
    'meter',
    'option',
    'progressbar',
    'radio',
    'radiogroup',
    'region',
    'rowheader',
    'searchbox',
    'slider',
    'spinbutton',
    'switch',
    'tab',
    'table',
    'tabpanel',
    'textbox',
    'tooltip',
    'tree',
    'treegrid',
    'treeitem',
]);
/**
 * Replace Playwright's missing-browser banner with the command that fixes it.
 *
 * The browser comes from the project's own Playwright, so the install command
 * is the project's: run in the directory 9l was started in, it installs the
 * build that Playwright version needs.
 */
function missingBrowserError(error, browserName) {
    const message = error instanceof Error ? error.message : String(error);
    if (!/Executable doesn't exist/.test(message))
        return undefined;
    return new Error(`The ${browserName} build for this project's Playwright is not installed. ` +
        `Install the matching build with: ${(0, doctor_1.browserInstallCommand)(browserName)}. ` +
        `On Linux, if system libraries are missing, rerun the same command with --with-deps.`);
}
function browserContextOptions(viewport) {
    return { viewport: viewport ?? exports.DEFAULT_VIEWPORT, serviceWorkers: 'block' };
}
async function withPage(options, fn) {
    const initialUrl = await (0, network_policy_1.assertSafeNetworkUrl)(options.url, { allowPrivateNetwork: options.allowPrivateNetwork });
    if (options.storageStatePath && options.acknowledgePrivateContent !== true) {
        throw new Error('Authenticated inspection requires acknowledgePrivateContent:true because the result may contain private page content.');
    }
    const storageStatePath = options.storageStatePath
        ? await resolveWorkspaceStorageStatePath(options.storageStatePath)
        : undefined;
    const browserName = options.browser ?? 'chromium';
    const policy = { allowPrivateNetwork: options.allowPrivateNetwork, privateNetworkOrigin: initialUrl.origin };
    // Every connection the browser makes, redirects included, goes through the
    // policy proxy; `<-loopback>` keeps loopback destinations from bypassing it.
    const proxy = await (0, policy_proxy_1.startPolicyProxy)(policy);
    let browser;
    try {
        browser = await browserType(browserName).launch({
            headless: !options.headed,
            proxy: { server: proxy.server, bypass: '<-loopback>' },
        });
    }
    catch (error) {
        await proxy.close();
        throw missingBrowserError(error, browserName) ?? error;
    }
    try {
        let context;
        try {
            context = await browser.newContext({
                ...browserContextOptions(options.viewport),
                ...(storageStatePath ? { storageState: storageStatePath } : {}),
            });
        }
        catch (error) {
            if (storageStatePath) {
                throw new Error('storageStatePath must contain valid Playwright storage-state JSON.');
            }
            throw error;
        }
        await enforceBrowserNetworkPolicy(context, policy);
        const page = await context.newPage();
        page.setDefaultTimeout(15_000);
        try {
            await page.goto(validateHttpUrl(options.url), { waitUntil: 'domcontentloaded', timeout: 30_000 });
        }
        catch (error) {
            // Do not echo target URLs, query parameters or Playwright call logs.
            if (error instanceof Error && (error.name === 'TimeoutError' || /net::ERR_|NS_ERROR_|Navigation failed/.test(error.message))) {
                throw new Error('Target could not be reached. Start the application and check its URL and published port. For an explicit loopback URL, opt in with --allow-private-network (MCP: allow_private_network:true).');
            }
            throw error;
        }
        return await fn(page, browser);
    }
    finally {
        await browser.close();
        await proxy.close();
    }
}
const MAX_STORAGE_STATE_BYTES = 10 * 1024 * 1024;
/**
 * Resolve a caller-selected workspace file without allowing reads outside the
 * workspace.
 *
 * Every caller-supplied path the server reads goes through here: the auth state,
 * and the scan baseline. A second path input reaching the filesystem on its own
 * terms is how traversal and symlink escapes get reintroduced one feature at a
 * time, so the guard takes the label rather than the check being copied.
 */
async function resolveWorkspaceFile(requestedPath, label, maxBytes) {
    if (node_path_1.default.isAbsolute(requestedPath)) {
        throw new Error(`${label} must be relative to the active workspace.`);
    }
    const workspaceRoot = await (0, promises_1.realpath)(process.cwd());
    let resolved;
    try {
        resolved = await (0, promises_1.realpath)(node_path_1.default.resolve(workspaceRoot, requestedPath));
    }
    catch {
        throw new Error(`${label} must name an existing workspace file.`);
    }
    assertWithin(workspaceRoot, resolved, `${label} escapes the active workspace.`);
    let metadata;
    try {
        metadata = await (0, promises_1.stat)(resolved);
    }
    catch {
        throw new Error(`${label} must name an existing workspace file.`);
    }
    if (!metadata.isFile()) {
        throw new Error(`${label} must name a regular file.`);
    }
    if (metadata.size > maxBytes) {
        throw new Error(`${label} exceeds the ${Math.round(maxBytes / (1024 * 1024))} MB safety limit.`);
    }
    return resolved;
}
/** Resolve a caller-selected Playwright auth state without allowing reads outside the workspace. */
async function resolveWorkspaceStorageStatePath(requestedPath) {
    return await resolveWorkspaceFile(requestedPath, 'storageStatePath', MAX_STORAGE_STATE_BYTES);
}
const MAX_BASELINE_BYTES = 10 * 1024 * 1024;
/**
 * Load a previous scan result to compare against.
 *
 * A baseline is just an earlier result, so it can be handed over inline by a
 * caller that still holds one or named as a workspace file by a pipeline that
 * persisted one. The file path is confined exactly as the auth state is.
 */
async function loadBaselineFindings(baseline) {
    let parsed = baseline;
    if (typeof baseline === 'string') {
        const resolved = await resolveWorkspaceFile(baseline, 'baseline', MAX_BASELINE_BYTES);
        try {
            parsed = JSON.parse(await (0, promises_1.readFile)(resolved, 'utf8'));
        }
        catch {
            throw new Error('baseline must contain the JSON result of a previous scan.');
        }
    }
    const findings = parsed?.findings;
    if (!Array.isArray(findings)) {
        throw new Error('baseline must contain the JSON result of a previous scan, including its findings array.');
    }
    return findings;
}
async function enforceBrowserNetworkPolicy(context, options) {
    await context.route('**/*', async (route) => {
        try {
            await (0, network_policy_1.assertSafeNetworkUrl)(route.request().url(), options);
            await route.continue();
        }
        catch {
            await route.abort('blockedbyclient');
        }
    });
    await context.routeWebSocket('**/*', async (webSocket) => {
        try {
            await (0, network_policy_1.assertSafeNetworkUrl)(webSocket.url(), options);
            webSocket.connectToServer();
        }
        catch {
            await webSocket.close({ code: 1008, reason: 'Blocked by local safety policy.' });
        }
    });
}
async function writeTempFile(prefix, extension, content) {
    const dir = node_path_1.default.join((0, node_os_1.tmpdir)(), '9lives-browser-checks');
    await (0, promises_1.mkdir)(dir, { recursive: true });
    const safePrefix = prefix.replace(/[^a-zA-Z0-9_-]/g, '-').slice(0, 40) || 'artifact';
    const file = node_path_1.default.join(dir, `${safePrefix}-${Date.now()}-${Math.random().toString(36).slice(2)}.${extension}`);
    await (0, promises_1.writeFile)(file, content, 'utf8');
    return file;
}
/** How many distinct URLs a grouped finding lists before counting the rest in `omittedUrls`. */
exports.MAX_GROUPED_URLS = 5;
/**
 * Collapse repeated observations of the same problem into one finding.
 *
 * A page that fires the same doomed request three times used to produce three
 * byte-identical findings — and, because the score charges a penalty per
 * finding, one misconfigured SDK firing on every render cost 3× the identical
 * misconfiguration firing once. Findings that agree on severity, category,
 * message, URL, and selector describe one problem, so they become one finding
 * whose `occurrences` says how often it was seen. The first observation keeps
 * its evidence and repro; the count is the only thing repetition adds.
 *
 * A finding the caller marks as page-wide (`acrossUrls`) is one problem
 * whatever URL it was seen on: a router aborting superseded prefetches does so
 * for every link it prefetches, and eleven findings that differ only in URL
 * say the same thing eleven times. Those merge on message alone and list
 * their URLs instead of carrying one each.
 */
function dedupeFindings(findings, options = {}) {
    const byKey = new Map();
    const urlsByKey = new Map();
    for (const finding of findings) {
        const grouped = options.acrossUrls?.(finding) === true;
        const key = JSON.stringify([
            finding.severity,
            finding.category,
            finding.message,
            grouped ? null : finding.url,
            finding.selector,
            grouped,
        ]);
        const existing = byKey.get(key);
        if (existing) {
            existing.occurrences = (existing.occurrences ?? 1) + 1;
        }
        else {
            const copy = { ...finding };
            if (grouped)
                delete copy.url;
            byKey.set(key, copy);
        }
        if (grouped && finding.url) {
            const urls = urlsByKey.get(key) ?? new Set();
            urls.add(finding.url);
            urlsByKey.set(key, urls);
        }
    }
    for (const [key, urls] of urlsByKey) {
        const finding = byKey.get(key);
        const distinct = Array.from(urls);
        finding.urls = distinct.slice(0, exports.MAX_GROUPED_URLS);
        if (distinct.length > exports.MAX_GROUPED_URLS)
            finding.omittedUrls = distinct.length - exports.MAX_GROUPED_URLS;
    }
    return Array.from(byKey.values());
}
/**
 * A stable identity for a problem, so the same problem hashes the same across
 * runs and two scans can be compared by machine.
 *
 * Severity is deliberately excluded. A finding whose severity is reclassified —
 * as prefetch aborts were, from medium to info — is still the same problem, and
 * a diff that reported it as one finding fixed and another appearing would be
 * describing a change nobody made. `occurrences` is excluded for the same
 * reason: seeing a problem four times instead of three is not a different
 * problem.
 *
 * The message is compared verbatim apart from whitespace and case, which means
 * a finding that embeds a count ("6 render-blocking resources") gets a new
 * identity when that count moves. That is the conservative direction: it shows
 * up as one fixed and one new rather than silently reporting "unchanged" for a
 * finding whose text no longer matches.
 *
 * A finding grouped across URLs by `dedupeFindings` has no `url`, so its
 * identity is its category and message: the same whichever prefetch the page
 * happened to abort first, and whether it aborted three or eleven.
 */
function findingFingerprint(finding) {
    const normalize = (value) => (value ?? '').replace(/\s+/g, ' ').trim().toLowerCase();
    const material = JSON.stringify([
        normalize(finding.category),
        normalize(finding.message),
        normalize(finding.url),
        normalize(finding.selector),
    ]);
    return (0, node_crypto_1.createHash)('sha256').update(material).digest('hex').slice(0, 12);
}
/** Attach a fingerprint to every finding that does not already carry one. */
function withFingerprints(findings) {
    return findings.map((finding) => ({ ...finding, id: finding.id ?? findingFingerprint(finding) }));
}
/**
 * Compare a scan against a baseline scan of the same URL.
 *
 * The workflow `scan_url` is documented for — scan, change something, scan
 * again, decide — is a comparison, and doing it by eye across two JSON blobs
 * makes "did my change introduce anything new?" depend on the reader's
 * diligence. It also blocks CI: a pipeline can gate on "new findings since
 * baseline" but not on "findingCount > 0" once known-benign findings exist.
 */
function diffFindings(current, baseline) {
    const currentById = new Map(withFingerprints(current).map((finding) => [finding.id, finding]));
    const baselineById = new Map(withFingerprints(baseline).map((finding) => [finding.id, finding]));
    const fresh = [...currentById].filter(([id]) => !baselineById.has(id)).map(([, finding]) => finding);
    const unchanged = [...currentById].filter(([id]) => baselineById.has(id)).map(([, finding]) => finding);
    const fixed = [...baselineById].filter(([id]) => !currentById.has(id)).map(([, finding]) => finding);
    const count = (items, noun) => `${items.length} ${noun}${items.length === 1 ? '' : 's'}`;
    const verdict = fresh.length === 0
        ? `No new findings since baseline${fixed.length > 0 ? `; ${count(fixed, 'finding')} fixed` : ''}.`
        : `${count(fresh, 'new finding')} since baseline${fixed.length > 0 ? `, ${count(fixed, 'finding')} fixed` : ''}.`;
    return { fixed, new: fresh, unchanged, verdict };
}
function scoreFromFindings(findings) {
    const penalty = findings.reduce((sum, finding) => sum + exports.SEVERITY_WEIGHTS[finding.severity], 0);
    return Math.max(0, Math.min(100, 100 - penalty));
}
function cssEscape(value) {
    return value.replace(/\\/g, '\\\\').replace(/"/g, '\\"');
}
function assertWithin(root, candidate, message) {
    const relative = node_path_1.default.relative(root, candidate);
    if (!relative || relative === '..' || relative.startsWith(`..${node_path_1.default.sep}`) || node_path_1.default.isAbsolute(relative)) {
        throw new Error(message);
    }
}
