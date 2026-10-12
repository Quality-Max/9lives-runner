"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.installVitalsObservers = installVitalsObservers;
exports.readVitals = readVitals;
exports.analyzeVitals = analyzeVitals;
/**
 * Registered before navigation so the observers see buffered entries from the very first paint.
 * Runs in the page, not in Node.
 */
function installVitalsObservers() {
    const store = { lcpMs: null, clsScore: 0 };
    window['__nineLivesVitals'] = store;
    try {
        new PerformanceObserver((list) => {
            for (const entry of list.getEntries())
                store.lcpMs = entry.startTime;
        }).observe({ type: 'largest-contentful-paint', buffered: true });
    }
    catch {
        // Browser does not support the entry type; the metric stays null rather than being guessed.
    }
    try {
        new PerformanceObserver((list) => {
            for (const entry of list.getEntries()) {
                const shift = entry;
                if (!shift.hadRecentInput)
                    store.clsScore += shift.value;
            }
        }).observe({ type: 'layout-shift', buffered: true });
    }
    catch {
        // Same as above.
    }
}
/** Read the observed vitals plus navigation and paint timings. Runs in the page. */
function readVitals() {
    const store = (window['__nineLivesVitals'] ?? {});
    const navigation = performance.getEntriesByType('navigation')[0];
    const paint = performance.getEntriesByName('first-contentful-paint')[0];
    return {
        lcpMs: typeof store.lcpMs === 'number' ? Math.round(store.lcpMs) : null,
        clsScore: typeof store.clsScore === 'number' ? Number(store.clsScore.toFixed(4)) : null,
        fcpMs: paint ? Math.round(paint.startTime) : null,
        ttfbMs: navigation ? Math.round(navigation.responseStart - navigation.startTime) : null,
        domContentLoadedMs: navigation ? Math.round(navigation.domContentLoadedEventEnd - navigation.startTime) : null,
        loadMs: navigation ? Math.round(navigation.loadEventEnd - navigation.startTime) : null,
    };
}
/** Google's Core Web Vitals thresholds. */
const THRESHOLDS = [
    {
        key: 'lcpMs',
        label: 'Largest Contentful Paint',
        good: 2500,
        poor: 4000,
        format: (value) => `${value}ms`,
        suggestion: 'Preload the hero image or font, cut render-blocking resources, and improve server response time.',
    },
    {
        key: 'clsScore',
        label: 'Cumulative Layout Shift',
        good: 0.1,
        poor: 0.25,
        format: (value) => value.toFixed(3),
        suggestion: 'Reserve space for images, ads, and embeds with explicit width/height or aspect-ratio.',
    },
    {
        key: 'ttfbMs',
        label: 'Time to First Byte',
        good: 800,
        poor: 1800,
        format: (value) => `${value}ms`,
        suggestion: 'Cache at the edge, reduce server work on the critical path, and check redirect chains.',
    },
    {
        key: 'fcpMs',
        label: 'First Contentful Paint',
        good: 1800,
        poor: 3000,
        format: (value) => `${value}ms`,
        suggestion: 'Reduce render-blocking CSS and JavaScript so the browser can paint sooner.',
    },
];
/**
 * Grade Core Web Vitals sampled during the scan.
 *
 * The sample is a single cold load from the scanning machine, not field data, and INP is not
 * measured at all — both are stated in `metrics.notes` rather than papered over.
 */
function analyzeVitals(sample, pageUrl) {
    const findings = [];
    const notes = [
        'Sampled from one cold load on the scanning machine. Treat it as a signal, not as field data from real users.',
        'INP is not measured: Interaction to Next Paint requires real user interaction, which this scan does not perform.',
    ];
    if (sample.lcpMs !== null) {
        notes.push('LCP is its value once the network went idle; a later element could still become the largest.');
    }
    for (const threshold of THRESHOLDS) {
        const value = sample[threshold.key];
        if (value === null)
            continue;
        if (value <= threshold.good)
            continue;
        const poor = value > threshold.poor;
        findings.push({
            severity: poor ? (value > threshold.poor * 2 ? 'high' : 'medium') : 'low',
            category: 'performance',
            message: `${threshold.label} is ${threshold.format(value)} (${poor ? 'poor' : 'needs improvement'}; good is ${threshold.format(threshold.good)} or less).`,
            evidence: { value, good: threshold.good, poor: threshold.poor },
            repro: `1. Open ${pageUrl} in an incognito window\n2. Open DevTools → Lighthouse or Performance\n3. Reload with an empty cache and read ${threshold.label}`,
            suggestion: threshold.suggestion,
        });
    }
    return { findings, metrics: { ...sample, inpMs: null, notes } };
}
