"use strict";
/**
 * Shared, browser-free inputs for the scan analyzers.
 *
 * Everything here is collected during the single page load `scanUrl` already performs, so the
 * analyzers stay pure functions that can be unit-tested without launching Playwright.
 */
Object.defineProperty(exports, "__esModule", { value: true });
exports.mergeResourceSignals = mergeResourceSignals;
exports.transferBytes = transferBytes;
exports.formatBytes = formatBytes;
/** PerformanceResourceTiming initiator types mapped onto Playwright resource types. */
const INITIATOR_RESOURCE_TYPE = {
    navigation: 'document',
    iframe: 'document',
    frame: 'document',
    script: 'script',
    link: 'stylesheet',
    img: 'image',
    image: 'image',
    input: 'image',
    video: 'media',
    audio: 'media',
    track: 'media',
    xmlhttprequest: 'xhr',
    fetch: 'fetch',
    beacon: 'fetch',
    css: 'other',
};
/**
 * Join what the network layer saw with what the page's own resource timing reports.
 *
 * Responses carry the authoritative resource type and headers; resource timing carries the byte
 * counts and durations. Entries present in only one source are still included, so a resource that
 * never produced a response event is not silently dropped from the weight total.
 */
function mergeResourceSignals(responses, timings, toDisplayUrl) {
    const timingByUrl = new Map(timings.map((timing) => [timing.url, timing]));
    const merged = new Map();
    const sizesUsable = (timing) => Boolean(timing && ((timing.transferSize ?? 0) > 0 || (timing.encodedBodySize ?? 0) > 0));
    for (const response of responses) {
        const timing = timingByUrl.get(response.url);
        merged.set(response.url, {
            ...response,
            url: toDisplayUrl(response.url),
            transferSize: timing?.transferSize,
            encodedBodySize: timing?.encodedBodySize,
            decodedBodySize: timing?.decodedBodySize,
            startMs: timing?.startMs,
            durationMs: timing?.durationMs,
            timingAvailable: sizesUsable(timing),
        });
    }
    for (const timing of timings) {
        if (merged.has(timing.url))
            continue;
        merged.set(timing.url, {
            url: toDisplayUrl(timing.url),
            resourceType: INITIATOR_RESOURCE_TYPE[timing.initiatorType ?? ''] ?? 'other',
            transferSize: timing.transferSize,
            encodedBodySize: timing.encodedBodySize,
            decodedBodySize: timing.decodedBodySize,
            startMs: timing.startMs,
            durationMs: timing.durationMs,
            timingAvailable: sizesUsable(timing),
        });
    }
    return Array.from(merged.values());
}
/**
 * Best available transfer size for one resource.
 *
 * Prefers real resource timing, falls back to the `content-length` header, and reports 0 rather
 * than guessing when neither is available.
 */
function transferBytes(resource) {
    if (resource.timingAvailable && typeof resource.transferSize === 'number' && resource.transferSize > 0) {
        return resource.transferSize;
    }
    if (typeof resource.contentLength === 'number' && resource.contentLength > 0) {
        return resource.contentLength;
    }
    if (typeof resource.encodedBodySize === 'number' && resource.encodedBodySize > 0) {
        return resource.encodedBodySize;
    }
    return 0;
}
/** Format a byte count for finding messages. */
function formatBytes(bytes) {
    if (bytes >= 1_000_000)
        return `${(bytes / 1_000_000).toFixed(1)} MB`;
    if (bytes >= 1_000)
        return `${Math.round(bytes / 1_000)} kB`;
    return `${bytes} B`;
}
