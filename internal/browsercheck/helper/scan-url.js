"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.BENIGN_ABORT_MESSAGE = exports.SUPPORTED_CHECKS = void 0;
exports.resolveChecks = resolveChecks;
exports.isBenignNavigationAbort = isBenignNavigationAbort;
exports.isPageWideFinding = isPageWideFinding;
exports.scanUrl = scanUrl;
exports.looksStateChanging = looksStateChanging;
exports.checkSecurityHeaders = checkSecurityHeaders;
const common_1 = require("./common");
const cookies_1 = require("./checks/cookies");
const telemetry_1 = require("./checks/telemetry");
const mixed_content_1 = require("./checks/mixed-content");
const signals_1 = require("./checks/signals");
const vitals_1 = require("./checks/vitals");
const weight_1 = require("./checks/weight");
const network_policy_1 = require("./network-policy");
const redact_1 = require("./redact");
/**
 * Every check `scan_url` knows how to run, and the set it runs when the caller
 * names none. Exported so the tool schema and the caller-facing error message
 * are generated from one list rather than three copies of it.
 */
exports.SUPPORTED_CHECKS = [
    'console',
    'links',
    'accessibility',
    'performance',
    'seo',
    'security_headers',
    'cookies',
    'mixed_content',
    'weight',
];
const DEFAULT_CHECKS = exports.SUPPORTED_CHECKS;
/**
 * Resolve the requested check names, rejecting any this build does not know.
 *
 * An unrecognised name used to be dropped silently: it matched no `checks.has()`
 * branch, so the check never ran, yet the name was still echoed back in the
 * response. A caller who misspelled one — `security-headers` for
 * `security_headers`, say — got a scan that skipped it, and if *every* name was
 * unrecognised the scan ran nothing at all and still scored 100, which is
 * indistinguishable from a genuinely clean page. Anything gating on `score` or
 * `findingCount` therefore failed open. Fail loudly instead; a typo is a caller
 * bug worth surfacing, not a reason to report success.
 */
function resolveChecks(requested) {
    if (!requested || requested.length === 0)
        return new Set(DEFAULT_CHECKS);
    const normalized = requested.map((item) => item.trim().toLowerCase()).filter((item) => item.length > 0);
    if (normalized.length === 0)
        return new Set(DEFAULT_CHECKS);
    const unknown = normalized.filter((item) => !exports.SUPPORTED_CHECKS.includes(item));
    if (unknown.length > 0) {
        throw new Error(`Unknown scan check(s): ${unknown.join(', ')}. Supported checks: ${exports.SUPPORTED_CHECKS.join(', ')}.`);
    }
    return new Set(normalized);
}
/** Checks that need the page's own network and markup inventory. */
const PAGE_INVENTORY_CHECKS = ['cookies', 'mixed_content', 'weight'];
/**
 * Is this failed request the page's own navigation machinery cancelling itself?
 *
 * Next.js App Router (and routers like it) abort `<Link>` prefetches and RSC
 * payload fetches whenever a navigation supersedes them. That happens on
 * essentially every page, is invisible to users, and is not something the page
 * author can fix — yet each abort was reported as a medium finding, so a clean
 * App Router page capped at 80 and the score stopped meaning anything. Only
 * deliberate aborts with a prefetch/RSC fingerprint qualify; a genuine
 * `ERR_ABORTED` on an asset or XHR the page needed keeps its severity.
 *
 * Must run against the raw request URL: `safeUrlForDisplay` strips the query,
 * and `?_rsc=` is one of the fingerprints.
 */
function isBenignNavigationAbort(failed) {
    if (failed.failure !== 'net::ERR_ABORTED')
        return false;
    if (failed.resourceType === 'prefetch')
        return true;
    const headers = failed.headers ?? {};
    const purpose = headers['sec-purpose'] ?? headers['purpose'] ?? '';
    if (purpose.includes('prefetch'))
        return true;
    // The Next.js RSC fetch signature: an `RSC: 1` request header, and a `_rsc`
    // cache-busting search param on the same request.
    if (headers['rsc'] === '1')
        return true;
    try {
        if (new URL(failed.url).searchParams.has('_rsc'))
            return true;
    }
    catch {
        return false;
    }
    return false;
}
/** The message every router-initiated abort shares; `isPageWideFinding` groups on it. */
exports.BENIGN_ABORT_MESSAGE = 'Request aborted by the page itself: a prefetch or RSC fetch superseded by navigation.';
/**
 * Is this finding about the page rather than about one URL on it?
 *
 * A router that aborts superseded prefetches does so for every link it
 * prefetches, so one App Router page produced eleven identical info findings
 * that differed only in URL. They are one observation about how the page
 * navigates; `dedupeFindings` merges them and lists the URLs. A genuine
 * failure stays one finding per URL, because each may need its own fix.
 */
function isPageWideFinding(finding) {
    return finding.category === 'network' && finding.severity === 'info' && finding.message === exports.BENIGN_ABORT_MESSAGE;
}
async function scanUrl(args) {
    const url = (0, common_1.validateHttpUrl)(args.url);
    const displayUrl = (0, network_policy_1.safeUrlForDisplay)(url);
    const checks = resolveChecks(args.checks);
    const findings = [];
    const consoleMessages = [];
    const requestFailures = [];
    let mainHeaders = {};
    let screenshotPath;
    const metrics = {};
    const wantsInventory = PAGE_INVENTORY_CHECKS.some((check) => checks.has(check));
    await (0, common_1.withPage)({
        url,
        viewport: args.viewport,
        allowPrivateNetwork: args.allowPrivateNetwork,
        storageStatePath: args.storageStatePath,
        acknowledgePrivateContent: args.acknowledgePrivateContent,
    }, async (page) => {
        const responses = [];
        if (wantsInventory) {
            page.on('response', (response) => {
                const headers = response.headers();
                const declaredLength = Number.parseInt(headers['content-length'] ?? '', 10);
                responses.push({
                    url: response.url(),
                    resourceType: response.request().resourceType(),
                    status: response.status(),
                    contentType: headers['content-type'],
                    contentEncoding: headers['content-encoding'],
                    contentLength: Number.isFinite(declaredLength) ? declaredLength : undefined,
                });
            });
        }
        if (checks.has('performance')) {
            await page.addInitScript(vitals_1.installVitalsObservers);
        }
        page.on('console', (msg) => {
            if (['error', 'warning'].includes(msg.type())) {
                consoleMessages.push({
                    type: msg.type(),
                    text: msg.text().slice(0, 1000),
                    location: msg.location(),
                });
            }
        });
        page.on('pageerror', (err) => {
            consoleMessages.push({ type: 'error', text: err.message.slice(0, 1000) });
        });
        page.on('requestfailed', (request) => {
            requestFailures.push({
                url: request.url(),
                failure: request.failure()?.errorText,
                resourceType: request.resourceType(),
                headers: request.headers(),
            });
        });
        const response = await page.reload({ waitUntil: 'networkidle', timeout: 30_000 }).catch(async () => {
            return await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 30_000 });
        });
        mainHeaders = response?.headers() ?? {};
        if (checks.has('console')) {
            for (const message of consoleMessages) {
                // A failed subresource is reported to the console with the request's own
                // URL as its location, and that URL is often the only place the cause is
                // named. A PostHog remote-config 404 arrives here and nowhere else: it is
                // a served response, so `requestfailed` never sees it.
                const locationUrl = message.location?.url;
                const signature = locationUrl ? (0, telemetry_1.identifyTelemetryRequest)(locationUrl) : null;
                if (signature) {
                    const telemetryUrl = (0, network_policy_1.safeUrlForDisplay)(locationUrl);
                    findings.push({
                        ...(0, telemetry_1.describeTelemetryFailure)(signature, telemetryUrl, message.text),
                        repro: `1. Open ${displayUrl}\n2. Open DevTools → Network\n3. Observe the request to ${telemetryUrl}: ${message.text}`,
                    });
                    continue;
                }
                findings.push({
                    severity: message.type === 'error' ? 'high' : 'medium',
                    category: 'console',
                    message: `${message.type}: ${message.text}`,
                    evidence: message.location,
                    repro: `1. Open ${displayUrl}\n2. Open DevTools → Console\n3. Observe: ${message.text}`,
                    suggestion: 'Fix runtime errors and warnings before relying on generated tests.',
                });
            }
            for (const failed of requestFailures.slice(0, 20)) {
                const benign = isBenignNavigationAbort(failed);
                // Name the SDK rather than the transport error. One SDK-level finding
                // replaces N anonymous "Request failed" findings, and because the
                // console surface produces the same message and URL for the same
                // request, deduplication merges the pair into a single finding.
                const signature = benign ? null : (0, telemetry_1.identifyTelemetryRequest)(failed.url);
                if (signature) {
                    const telemetryUrl = (0, network_policy_1.safeUrlForDisplay)(failed.url);
                    const failure = failed.failure ?? 'unknown error';
                    findings.push({
                        ...(0, telemetry_1.describeTelemetryFailure)(signature, telemetryUrl, failure),
                        repro: `1. Open ${displayUrl}\n2. Open DevTools → Network\n3. Observe the request to ${telemetryUrl}: ${failure}`,
                    });
                    continue;
                }
                // A benign abort is grouped with the others across URLs, so its repro
                // names the behaviour rather than this one request.
                findings.push({
                    severity: benign ? 'info' : 'medium',
                    category: 'network',
                    message: benign ? exports.BENIGN_ABORT_MESSAGE : `Request failed: ${failed.failure ?? 'unknown error'}`,
                    url: (0, network_policy_1.safeUrlForDisplay)(failed.url),
                    repro: benign
                        ? `1. Open ${displayUrl}\n2. Open DevTools → Network\n3. Observe prefetch and RSC requests end with net::ERR_ABORTED when a navigation supersedes them`
                        : `1. Open ${displayUrl}\n2. Open DevTools → Network\n3. Observe request to ${(0, network_policy_1.safeUrlForDisplay)(failed.url)} fails: ${failed.failure ?? 'unknown error'}`,
                    suggestion: benign
                        ? 'No action needed: routers abort superseded prefetch and RSC requests by design.'
                        : 'Verify the asset or API route is reachable in the tested environment.',
                });
            }
        }
        if (checks.has('links')) {
            findings.push(...(await checkLinks(pageUrl(url), await page.locator('a[href]').evaluateAll((els) => els.map((el) => el.href)), args.maxLinks ?? 50, Boolean(args.allowPrivateNetwork), new URL(url).origin, async (targetUrl) => await page.evaluate(async (href) => {
                const fetchStatus = async (method) => {
                    const controller = new AbortController();
                    const timeout = setTimeout(() => controller.abort(), 10_000);
                    try {
                        const response = await fetch(href, {
                            method,
                            credentials: 'same-origin',
                            redirect: 'follow',
                            signal: controller.signal,
                        });
                        const status = response.status;
                        await response.body?.cancel().catch(() => undefined);
                        return status;
                    }
                    finally {
                        clearTimeout(timeout);
                    }
                };
                try {
                    return await fetchStatus('HEAD');
                }
                catch {
                    try {
                        return await fetchStatus('GET');
                    }
                    catch {
                        return undefined;
                    }
                }
            }, targetUrl))));
        }
        if (checks.has('accessibility')) {
            const a11yIssues = await page.evaluate(() => {
                const issues = [];
                const selectorFor = (el) => {
                    const id = el.getAttribute('id');
                    if (id)
                        return `#${CSS.escape(id)}`;
                    const testId = el.getAttribute('data-testid') || el.getAttribute('data-test');
                    if (testId)
                        return `[data-testid="${testId}"]`;
                    // Fall back to a path rather than the bare tag name. Two unnamed
                    // anchors previously produced two findings both reading `selector: "a"`
                    // with the same repro step ("Inspect `a`"), which matches every link on
                    // the page — the finding was correct but identified nothing.
                    const parts = [];
                    let node = el;
                    while (node && parts.length < 6) {
                        const tag = node.tagName.toLowerCase();
                        if (tag === 'body' || tag === 'html') {
                            parts.unshift(tag);
                            break;
                        }
                        const nodeId = node.getAttribute('id');
                        if (nodeId) {
                            parts.unshift(`#${CSS.escape(nodeId)}`);
                            break;
                        }
                        const parent = node.parentElement;
                        if (!parent) {
                            parts.unshift(tag);
                            break;
                        }
                        const twins = Array.from(parent.children).filter((child) => child.tagName === node.tagName);
                        parts.unshift(twins.length > 1 ? `${tag}:nth-of-type(${twins.indexOf(node) + 1})` : tag);
                        node = parent;
                    }
                    return parts.join(' > ');
                };
                for (const img of Array.from(document.images)) {
                    if (!img.getAttribute('alt')) {
                        issues.push({
                            severity: 'medium',
                            category: 'accessibility',
                            message: 'Image is missing alt text.',
                            selector: selectorFor(img),
                            suggestion: 'Add meaningful alt text or alt="" for decorative images.',
                        });
                    }
                }
                for (const input of Array.from(document.querySelectorAll('input, textarea, select'))) {
                    const id = input.getAttribute('id');
                    const hasLabel = Boolean(input.getAttribute('aria-label') ||
                        input.getAttribute('aria-labelledby') ||
                        (id && document.querySelector(`label[for="${CSS.escape(id)}"]`)) ||
                        input.closest('label'));
                    if (!hasLabel && input.getAttribute('type') !== 'hidden') {
                        issues.push({
                            severity: 'high',
                            category: 'accessibility',
                            message: 'Form control has no accessible label.',
                            selector: selectorFor(input),
                            suggestion: 'Associate the control with a visible label or aria-label.',
                        });
                    }
                }
                // Controls built from non-interactive elements — an <img> or <svg> inside a
                // div with a click handler — are invisible to every check above: they are
                // not interactive as far as the DOM is concerned, so there is nothing to
                // report a missing accessible name on. They are also unusable, because
                // they cannot be focused or activated from a keyboard. This is the common
                // React shape, and a clean accessibility result was previously being read
                // as evidence that such controls were reachable.
                const focusableSelector = 'a[href],button,input,select,textarea,[tabindex]:not([tabindex="-1"])';
                const interactiveRoles = new Set([
                    'button',
                    'checkbox',
                    'link',
                    'menuitem',
                    'menuitemcheckbox',
                    'menuitemradio',
                    'option',
                    'radio',
                    'switch',
                    'tab',
                    'treeitem',
                ]);
                const seenClickable = new Set();
                for (const el of Array.from(document.querySelectorAll('div, span, li, img, svg, i, [role]'))) {
                    if (el.matches(focusableSelector))
                        continue;
                    const role = (el.getAttribute('role') || '').trim().split(/\s+/)[0];
                    const hasInteractiveRole = interactiveRoles.has(role);
                    if (!hasInteractiveRole && el.getAttribute('role'))
                        continue;
                    if (!hasInteractiveRole && getComputedStyle(el).cursor !== 'pointer')
                        continue;
                    if (el.closest(focusableSelector))
                        continue;
                    // Clickable label text is not a trap. A <label> forwards its click to
                    // its own control, so the keyboard route is to tab to that control and
                    // the pointer cursor is correct, accessible markup. Consent checkboxes
                    // are the common shape: on a real registration form this rule was
                    // reporting both consent labels as unreachable when the checkboxes were
                    // perfectly operable, which is exactly the markup a reviewer scrutinises
                    // hardest in a regulated product.
                    const label = el.closest('label');
                    if (label && (label.control || label.querySelector(focusableSelector)))
                        continue;
                    // A clickable wrapper around a real control is reachable through that
                    // control, so the wrapper is not what blocks a keyboard user. The
                    // ancestor direction is handled by the closest() check above; this is
                    // the descendant direction.
                    if (el.querySelector(focusableSelector))
                        continue;
                    // Report the outermost element of a clickable cluster, so an icon nested
                    // in a styled wrapper yields one finding rather than one per layer.
                    if (Array.from(seenClickable).some((seen) => seen.contains(el)))
                        continue;
                    seenClickable.add(el);
                    issues.push({
                        severity: 'medium',
                        category: 'accessibility',
                        message: 'Element appears clickable but cannot be focused or activated from a keyboard.',
                        selector: selectorFor(el),
                        suggestion: hasInteractiveRole
                            ? 'Prefer a native interactive element, or add tabindex="0" and the keyboard handling required by the ARIA role.'
                            : 'Use a <button>, or add role="button", tabindex="0" and Enter/Space handling, plus an accessible name.',
                    });
                }
                for (const el of Array.from(document.querySelectorAll('button, a[href], [role="button"], [role="link"]'))) {
                    const name = (el.textContent || el.getAttribute('aria-label') || el.getAttribute('title') || '').trim();
                    if (!name) {
                        issues.push({
                            severity: 'medium',
                            category: 'accessibility',
                            message: 'Interactive element has no accessible name.',
                            selector: selectorFor(el),
                            suggestion: 'Add visible text or aria-label.',
                        });
                    }
                }
                const h1Count = document.querySelectorAll('h1').length;
                if (h1Count !== 1) {
                    issues.push({
                        severity: h1Count === 0 ? 'medium' : 'low',
                        category: 'accessibility',
                        message: `Expected exactly one h1, found ${h1Count}.`,
                        suggestion: 'Use one clear h1 for the page topic.',
                    });
                }
                return issues;
            });
            for (const issue of a11yIssues) {
                findings.push({
                    ...issue,
                    repro: issue.selector
                        ? `1. Open ${displayUrl}\n2. Inspect \`${issue.selector}\`\n3. Note: ${issue.message}`
                        : `1. Open ${displayUrl}\n2. ${issue.message}`,
                });
            }
        }
        if (checks.has('seo')) {
            const seo = await page.evaluate(() => ({
                title: document.title,
                description: document.querySelector('meta[name="description"]')?.getAttribute('content') || '',
                h1Count: document.querySelectorAll('h1').length,
            }));
            if (!seo.title || seo.title.length < 10) {
                findings.push({
                    severity: 'low',
                    category: 'seo',
                    message: 'Page title is missing or very short.',
                    repro: `curl -s ${displayUrl} | grep -i '<title>'`,
                    suggestion: 'Add a descriptive <title> (10+ chars).',
                });
            }
            if (!seo.description || seo.description.length < 50) {
                findings.push({
                    severity: 'low',
                    category: 'seo',
                    message: 'Meta description is missing or very short.',
                    repro: `curl -s ${displayUrl} | grep -i '<meta name="description"'`,
                    suggestion: 'Add a <meta name="description"> (50–160 chars).',
                });
            }
        }
        if (checks.has('performance')) {
            const vitals = (0, vitals_1.analyzeVitals)(await page.evaluate(vitals_1.readVitals), displayUrl);
            findings.push(...vitals.findings);
            metrics.vitals = vitals.metrics;
        }
        if (checks.has('security_headers')) {
            findings.push(...checkSecurityHeaders(mainHeaders, displayUrl));
        }
        if (wantsInventory) {
            const inventory = await page.evaluate(collectPageInventory);
            const timings = await page.evaluate(readResourceTimings);
            const resources = (0, signals_1.mergeResourceSignals)(responses, timings, network_policy_1.safeUrlForDisplay);
            if (checks.has('cookies')) {
                findings.push(...(0, cookies_1.analyzeCookies)({
                    pageUrl: displayUrl,
                    cookies: (await page.context().cookies()).map(cookies_1.toCookieSignal),
                    requestUrls: resources.map((resource) => resource.url),
                    consentBanner: inventory.consentBanner,
                }));
            }
            if (checks.has('mixed_content')) {
                findings.push(...(0, mixed_content_1.analyzeMixedContent)({
                    pageUrl: displayUrl,
                    markup: inventory.markup.map((item) => ({ ...item, url: (0, network_policy_1.safeUrlForDisplay)(item.url) })),
                    resources,
                }));
            }
            if (checks.has('weight')) {
                const weight = (0, weight_1.analyzeWeight)({
                    pageUrl: displayUrl,
                    resources,
                    renderBlocking: inventory.renderBlocking.map((item) => ({ ...item, url: (0, network_policy_1.safeUrlForDisplay)(item.url) })),
                    budget: args.weightBudget,
                });
                findings.push(...weight.findings);
                metrics.weight = weight.metrics;
            }
        }
        if (args.screenshot) {
            const file = await (0, common_1.writeTempFile)('scan', 'png', '');
            await page.screenshot({ path: file, fullPage: true });
            screenshotPath = file;
        }
    });
    // Deduplicate before scoring so a problem observed N times is penalised once,
    // then fingerprint so the survivors can be compared against another run.
    const deduped = (0, common_1.withFingerprints)((0, common_1.dedupeFindings)(findings, { acrossUrls: isPageWideFinding }));
    const redacted = (0, redact_1.redactSensitiveData)(deduped);
    // Diff the redacted findings, not the raw ones: the baseline was written from
    // a previous response and so is already redacted. Comparing across the
    // boundary would report every redacted finding as both fixed and new.
    let delta;
    if (args.baseline !== undefined) {
        delta = (0, common_1.diffFindings)(redacted, await (0, common_1.loadBaselineFindings)(args.baseline));
    }
    const score = (0, common_1.scoreFromFindings)(deduped);
    return {
        url: displayUrl,
        score,
        scoring: (0, common_1.describeScoring)(score),
        checks: Array.from(checks),
        findingCount: deduped.length,
        findings: redacted,
        delta,
        metrics: Object.keys(metrics).length > 0 ? metrics : undefined,
        screenshotPath,
    };
}
/** Read every `PerformanceResourceTiming` entry plus the navigation itself. Runs in the page. */
function readResourceTimings() {
    const entries = performance.getEntriesByType('resource');
    const navigation = performance.getEntriesByType('navigation')[0];
    const toObservation = (entry) => ({
        url: entry.name,
        initiatorType: entry.initiatorType,
        transferSize: entry.transferSize,
        encodedBodySize: entry.encodedBodySize,
        decodedBodySize: entry.decodedBodySize,
        startMs: Math.round(entry.startTime),
        durationMs: Math.round(entry.duration),
    });
    const observations = entries.map(toObservation);
    if (navigation)
        observations.unshift({ ...toObservation(navigation), url: location.href, initiatorType: 'navigation' });
    return observations;
}
/**
 * Read subresource references, render-blocking `<head>` resources, and any consent banner
 * straight out of the DOM. Runs in the page.
 *
 * Markup is read as well as the network because the browser refuses to load active mixed content,
 * so blocked subresources never produce a response event.
 */
function collectPageInventory() {
    const markup = [];
    const push = (kind, url) => {
        if (url && /^(https?|wss?):/i.test(url))
            markup.push({ kind, url });
    };
    for (const el of Array.from(document.querySelectorAll('script[src]')))
        push('script', el.src);
    for (const el of Array.from(document.querySelectorAll('link[rel~="stylesheet"][href]')))
        push('stylesheet', el.href);
    for (const el of Array.from(document.querySelectorAll('iframe[src]')))
        push('iframe', el.src);
    for (const el of Array.from(document.images))
        push('image', el.src);
    for (const el of Array.from(document.querySelectorAll('video[src], audio[src], source[src]')))
        push('media', el.src);
    for (const form of Array.from(document.forms))
        push('form-action', form.action);
    const renderBlocking = [];
    for (const el of Array.from(document.head.querySelectorAll('script[src]'))) {
        const script = el;
        if (!script.async && !script.defer && script.type !== 'module')
            renderBlocking.push({ kind: 'script', url: script.src });
    }
    for (const el of Array.from(document.head.querySelectorAll('link[rel~="stylesheet"][href]'))) {
        const link = el;
        if (!link.disabled && link.media !== 'print')
            renderBlocking.push({ kind: 'stylesheet', url: link.href });
    }
    // A consent banner is an overlay that names cookies/consent and offers an accept or reject
    // control. Requiring fixed or sticky positioning keeps ordinary footer privacy links, which are
    // not consent gates, from being reported as one.
    const consentText = /\b(cookie|consent|gdpr|ccpa)\b/i;
    const consentAction = /\b(accept|agree|allow|reject|decline|got it|understood)\b/i;
    let consentBanner = { present: false };
    const candidates = Array.from(document.querySelectorAll('div, section, aside, dialog, [role="dialog"]')).slice(0, 500);
    for (const el of candidates) {
        const rect = el.getBoundingClientRect();
        if (rect.width < 200 || rect.height < 40)
            continue;
        const style = getComputedStyle(el);
        const overlay = style.position === 'fixed' || style.position === 'sticky' || el.tagName === 'DIALOG';
        if (!overlay || style.visibility === 'hidden' || style.display === 'none')
            continue;
        const text = (el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 2000);
        if (!consentText.test(text))
            continue;
        const actions = Array.from(el.querySelectorAll('button, a, [role="button"]'));
        if (!actions.some((action) => consentAction.test(action.textContent || '')))
            continue;
        const id = el.getAttribute('id');
        const classes = (el.getAttribute('class') || '').trim().split(/\s+/).filter(Boolean).slice(0, 2);
        consentBanner = {
            present: true,
            selector: id ? `#${id}` : [el.tagName.toLowerCase(), ...classes].join('.'),
            excerpt: text.slice(0, 160),
        };
        break;
    }
    return { markup, renderBlocking, consentBanner };
}
function pageUrl(url) {
    return new URL(url);
}
async function checkLinks(baseUrl, hrefs, maxLinks, allowPrivateNetwork, privateNetworkOrigin, authenticatedSameOriginStatus) {
    const findings = [];
    const unique = Array.from(new Set(hrefs))
        .filter((href) => href.startsWith('http://') || href.startsWith('https://'))
        .slice(0, Math.max(0, maxLinks));
    await withConcurrency(unique, 5, async (href) => {
        try {
            const url = new URL(href, baseUrl);
            let status;
            if (url.origin === baseUrl.origin && authenticatedSameOriginStatus) {
                // A same-origin link is requested with the page's session, and many
                // applications sign out or change data on a GET (or a HEAD routed to
                // it). Requesting such a link would act as the user, so it is skipped
                // and reported instead of checked.
                if (looksStateChanging(url)) {
                    findings.push({
                        severity: 'info',
                        category: 'links',
                        message: 'Link not requested with the session because it looks like it signs out or changes data.',
                        url: (0, network_policy_1.safeUrlForDisplay)(url),
                        suggestion: 'Check this link by hand if it should be verified.',
                    });
                    return;
                }
                status = await authenticatedSameOriginStatus(url.toString());
            }
            if (status === undefined) {
                const policy = { allowPrivateNetwork, privateNetworkOrigin };
                const res = await (0, network_policy_1.safeFetch)(url, { method: 'HEAD' }, policy).catch(() => (0, network_policy_1.safeFetch)(url, { method: 'GET' }, policy));
                status = res.status;
            }
            if (status >= 400) {
                findings.push({
                    severity: status >= 500 ? 'high' : 'medium',
                    category: 'links',
                    message: `Link returned HTTP ${status}.`,
                    url: (0, network_policy_1.safeUrlForDisplay)(url),
                    suggestion: 'Update, redirect, or remove the broken link.',
                });
            }
        }
        catch (err) {
            findings.push({
                severity: 'medium',
                category: 'links',
                message: `Link check failed: ${err instanceof Error ? err.message : 'network request failed'}`,
                url: (0, network_policy_1.safeUrlForDisplay)(href),
            });
        }
    });
    return findings;
}
const STATE_CHANGING_LINK = /(?:^|[/_.?=&-])(?:log-?out|log_out|log-?off|sign-?out|sign_out|delete|destroy|remove|unsubscribe|revoke|deactivate|disable)(?:$|[/_.?=&-])/i;
/** Whether a same-origin link's path or query names a sign-out or a data change. */
function looksStateChanging(url) {
    let target = `${url.pathname}${url.search}`;
    try {
        target = decodeURIComponent(target);
    }
    catch {
        // Keep the encoded form; a malformed escape cannot hide a keyword that matters.
    }
    return STATE_CHANGING_LINK.test(target);
}
async function withConcurrency(items, limit, worker) {
    let next = 0;
    await Promise.all(Array.from({ length: Math.min(limit, items.length) }, async () => {
        while (next < items.length) {
            const item = items[next++];
            await worker(item);
        }
    }));
}
function checkSecurityHeaders(headers, url) {
    const required = [
        ['content-security-policy', 'Add a Content-Security-Policy to reduce script injection risk.', 'medium'],
        ['strict-transport-security', 'Add HSTS on HTTPS sites.', 'medium'],
        ['x-content-type-options', 'Add X-Content-Type-Options: nosniff.', 'low'],
        ['referrer-policy', 'Add a Referrer-Policy header.', 'low'],
    ];
    const isHttps = url.startsWith('https://');
    const findings = [];
    for (const [header, suggestion, severity] of required) {
        if (headers[header])
            continue;
        // Browsers ignore Strict-Transport-Security delivered over plain HTTP, so on
        // an http:// target its absence is not a defect the page can fix — it is a
        // question the scan cannot answer yet. Reporting it as a medium finding made
        // every local scan carry an unfixable penalty, which trains readers to
        // discount the grade. `mixed_content` already downgrades itself to info for
        // the same reason; this keeps the two consistent.
        if (header === 'strict-transport-security' && !isHttps) {
            findings.push({
                severity: 'info',
                category: 'security_headers',
                message: 'HSTS does not apply: the page itself was served over HTTP.',
                suggestion: 'Serve the page over HTTPS, then rescan to check for Strict-Transport-Security.',
            });
            continue;
        }
        findings.push({
            severity,
            category: 'security_headers',
            message: `Missing ${header} header.`,
            repro: `curl -sI ${url} | grep -i ${header}`,
            suggestion,
        });
    }
    return findings;
}
