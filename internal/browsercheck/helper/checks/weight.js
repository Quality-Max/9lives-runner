"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.DEFAULT_WEIGHT_BUDGET = void 0;
exports.analyzeWeight = analyzeWeight;
const signals_1 = require("./signals");
const trackers_1 = require("./trackers");
/** Default budget. Roughly a mid-range mobile connection staying interactive on first load. */
exports.DEFAULT_WEIGHT_BUDGET = {
    totalBytes: 2_000_000,
    requestCount: 80,
    renderBlocking: 5,
    imageBytes: 500_000,
    scriptBytes: 250_000,
};
const COMPRESSIBLE_TYPE = /^(text\/|application\/(javascript|json|xml|x-javascript)|image\/svg\+xml)/i;
const COMPRESSED_ENCODING = /\b(gzip|br|deflate|zstd)\b/i;
const UNCOMPRESSED_FLOOR_BYTES = 10_000;
/** Measure page weight, third-party cost, and the slowest requests against a budget. */
function analyzeWeight(input) {
    const budget = { ...exports.DEFAULT_WEIGHT_BUDGET, ...input.budget };
    const pageHost = (0, trackers_1.hostOf)(input.pageUrl);
    const findings = [];
    let totalBytes = 0;
    let totalBytesComplete = true;
    const bytesByResourceType = {};
    const thirdParty = new Map();
    for (const resource of input.resources) {
        const bytes = (0, signals_1.transferBytes)(resource);
        if (bytes === 0 && !resource.timingAvailable)
            totalBytesComplete = false;
        totalBytes += bytes;
        bytesByResourceType[resource.resourceType] = (bytesByResourceType[resource.resourceType] ?? 0) + bytes;
        const host = (0, trackers_1.hostOf)(resource.url);
        if (!host || !pageHost || !(0, trackers_1.isThirdParty)(host, pageHost))
            continue;
        const entry = thirdParty.get(host) ?? { host, bytes: 0, requests: 0, tracker: (0, trackers_1.identifyTracker)(host)?.name };
        entry.bytes += bytes;
        entry.requests += 1;
        thirdParty.set(host, entry);
    }
    const thirdPartyOrigins = Array.from(thirdParty.values())
        .sort((a, b) => b.bytes - a.bytes)
        .slice(0, 10);
    const slowestRequests = input.resources
        .filter((resource) => typeof resource.durationMs === 'number')
        .sort((a, b) => (b.durationMs ?? 0) - (a.durationMs ?? 0))
        .slice(0, 10)
        .map((resource) => ({
        url: resource.url,
        durationMs: Math.round(resource.durationMs ?? 0),
        bytes: (0, signals_1.transferBytes)(resource),
        resourceType: resource.resourceType,
    }));
    const metrics = {
        totalBytes,
        requestCount: input.resources.length,
        totalBytesComplete,
        bytesByResourceType,
        thirdPartyOrigins,
        slowestRequests,
        budget,
    };
    const networkSteps = `1. Open ${input.pageUrl}\n2. Open DevTools → Network with cache disabled\n3. Reload and read the transferred total`;
    if (totalBytes > budget.totalBytes) {
        findings.push({
            severity: totalBytes > budget.totalBytes * 2 ? 'high' : 'medium',
            category: 'weight',
            message: `Page transfers ${(0, signals_1.formatBytes)(totalBytes)}, over the ${(0, signals_1.formatBytes)(budget.totalBytes)} budget.`,
            evidence: { totalBytes, budgetBytes: budget.totalBytes, totalBytesComplete, bytesByResourceType },
            repro: networkSteps,
            suggestion: 'Compress and split the largest resource types listed above before adding more to the page.',
        });
    }
    if (input.resources.length > budget.requestCount) {
        findings.push({
            severity: input.resources.length > budget.requestCount * 2 ? 'medium' : 'low',
            category: 'weight',
            message: `Page makes ${input.resources.length} requests, over the ${budget.requestCount} budget.`,
            evidence: { requestCount: input.resources.length, budgetRequests: budget.requestCount },
            repro: networkSteps,
            suggestion: 'Bundle or defer non-critical requests; each one costs a connection and a round trip.',
        });
    }
    if (input.renderBlocking.length > budget.renderBlocking) {
        findings.push({
            severity: 'medium',
            category: 'weight',
            message: `${input.renderBlocking.length} render-blocking resources in <head>, over the ${budget.renderBlocking} budget.`,
            evidence: input.renderBlocking.map((item) => `${item.kind}: ${item.url}`).slice(0, 20),
            repro: `1. Open ${input.pageUrl}\n2. View source\n3. Count <head> scripts without async/defer and non-print stylesheets`,
            suggestion: 'Add async or defer to head scripts, and inline or preload only the CSS needed for first paint.',
        });
    }
    const oversized = (types, limit) => input.resources.filter((resource) => types.includes(resource.resourceType) && (0, signals_1.transferBytes)(resource) > limit);
    const bigImages = oversized(['image'], budget.imageBytes);
    if (bigImages.length > 0) {
        findings.push({
            severity: 'medium',
            category: 'weight',
            message: `${bigImages.length} image${bigImages.length === 1 ? ' exceeds' : 's exceed'} ${(0, signals_1.formatBytes)(budget.imageBytes)}.`,
            evidence: bigImages.slice(0, 20).map((resource) => `${resource.url} — ${(0, signals_1.formatBytes)((0, signals_1.transferBytes)(resource))}`),
            repro: networkSteps,
            suggestion: 'Serve AVIF or WebP at the displayed dimensions and add width/height to avoid layout shift.',
        });
    }
    const bigScripts = oversized(['script'], budget.scriptBytes);
    if (bigScripts.length > 0) {
        findings.push({
            severity: 'medium',
            category: 'weight',
            message: `${bigScripts.length} script${bigScripts.length === 1 ? ' exceeds' : 's exceed'} ${(0, signals_1.formatBytes)(budget.scriptBytes)}.`,
            evidence: bigScripts.slice(0, 20).map((resource) => `${resource.url} — ${(0, signals_1.formatBytes)((0, signals_1.transferBytes)(resource))}`),
            repro: networkSteps,
            suggestion: 'Code-split the bundle and load rarely used routes on demand; large scripts also block the main thread.',
        });
    }
    const uncompressed = input.resources.filter((resource) => {
        if (!resource.contentType || !COMPRESSIBLE_TYPE.test(resource.contentType))
            return false;
        if (resource.contentEncoding && COMPRESSED_ENCODING.test(resource.contentEncoding))
            return false;
        return (0, signals_1.transferBytes)(resource) > UNCOMPRESSED_FLOOR_BYTES;
    });
    if (uncompressed.length > 0) {
        findings.push({
            severity: 'medium',
            category: 'weight',
            message: `${uncompressed.length} text resource${uncompressed.length === 1 ? ' is' : 's are'} served without compression.`,
            evidence: uncompressed.slice(0, 20).map((resource) => `${resource.url} — ${(0, signals_1.formatBytes)((0, signals_1.transferBytes)(resource))}`),
            repro: `1. Open ${input.pageUrl}\n2. Open DevTools → Network\n3. Check the content-encoding response header on the listed resources`,
            suggestion: 'Enable Brotli or gzip at the origin or CDN. Text assets typically shrink by 70% or more.',
        });
    }
    const heaviestThirdParty = thirdPartyOrigins[0];
    if (heaviestThirdParty && totalBytes > 0 && heaviestThirdParty.bytes > totalBytes * 0.25) {
        findings.push({
            severity: 'medium',
            category: 'weight',
            message: `Third-party origin ${heaviestThirdParty.host} accounts for ${(0, signals_1.formatBytes)(heaviestThirdParty.bytes)} of page weight.`,
            evidence: thirdPartyOrigins,
            repro: `1. Open ${input.pageUrl}\n2. Open DevTools → Network\n3. Sort by size and filter to ${heaviestThirdParty.host}`,
            suggestion: 'Load third-party scripts lazily or behind consent, and confirm the page still works when they fail.',
        });
    }
    return { findings, metrics };
}
