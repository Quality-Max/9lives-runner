"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.assertSafeNetworkUrl = assertSafeNetworkUrl;
exports.safeUrlForDisplay = safeUrlForDisplay;
exports.safeFetch = safeFetch;
const promises_1 = require("node:dns/promises");
const node_net_1 = __importDefault(require("node:net"));
const SAFE_REJECTION = 'The requested network destination is not permitted by the local safety policy.';
/** Validate a network target immediately before a browser or fetch request. */
async function assertSafeNetworkUrl(input, options = {}) {
    let url;
    try {
        url = typeof input === 'string' ? new URL(input) : input;
    }
    catch {
        throw new Error('URL must be a valid http or https URL.');
    }
    if (url.protocol !== 'http:' && url.protocol !== 'https:') {
        throw new Error('URL must use http or https.');
    }
    if (url.username || url.password) {
        throw new Error('URLs containing credentials are not permitted.');
    }
    const hostname = url.hostname.replace(/^\[|\]$/g, '');
    const addresses = node_net_1.default.isIP(hostname)
        ? [{ address: hostname, family: node_net_1.default.isIP(hostname) }]
        : await resolveHost(hostname, options.lookup);
    const loopbackAccessApproved = Boolean(options.allowPrivateNetwork) &&
        (!options.privateNetworkOrigin || url.origin === options.privateNetworkOrigin) &&
        isExplicitLoopbackHostname(hostname);
    if (!addresses.length || addresses.some(({ address }) => !isPermittedAddress(address, loopbackAccessApproved))) {
        throw new Error(rejectionFor(url, hostname, options));
    }
    return url;
}
/**
 * Say which rule refused a request, from the caller's own input only.
 *
 * The refusal used to read the same with and without consent, so a caller who
 * had allowed private networking for a Docker Compose service name could not
 * tell why it was still denied. The explanation is chosen from the consent flag
 * and the hostname as written, never from what the hostname resolved to: with
 * consent, a private answer and a failed lookup produce the same text, so the
 * message cannot be used to map an internal network.
 */
function rejectionFor(url, hostname, options) {
    const explicitLoopback = isExplicitLoopbackHostname(hostname);
    if (!options.allowPrivateNetwork) {
        return explicitLoopback
            ? `${SAFE_REJECTION} Loopback targets need explicit consent: allow_private_network: true (CLI: --allow-private-network), for deliberate local development testing only.`
            : SAFE_REJECTION;
    }
    if (explicitLoopback && options.privateNetworkOrigin && url.origin !== options.privateNetworkOrigin) {
        return `${SAFE_REJECTION} allow_private_network approves only the loopback origin it was given, ${options.privateNetworkOrigin}; another local origin or port stays denied.`;
    }
    if (explicitLoopback)
        return SAFE_REJECTION;
    return (`${SAFE_REJECTION} allow_private_network extends it only to localhost, 127.0.0.0/8 and ::1 written in the URL itself. ` +
        'Any other host stays denied when it resolves to a private, internal or loopback address, including a Docker Compose service name. ' +
        'To reach a local service, publish its port on the host and use http://localhost:<port>.');
}
function safeUrlForDisplay(input) {
    try {
        const url = typeof input === 'string' ? new URL(input) : new URL(input.toString());
        url.username = '';
        url.password = '';
        url.search = '';
        url.hash = '';
        return url.toString();
    }
    catch {
        return 'unavailable URL';
    }
}
async function safeFetch(input, init = {}, options = {}) {
    let current = await assertSafeNetworkUrl(input, options);
    const redirectPolicy = options.allowPrivateNetwork && !options.privateNetworkOrigin
        ? { ...options, privateNetworkOrigin: current.origin }
        : options;
    const maxRedirects = options.maxRedirects ?? 5;
    for (let redirects = 0; redirects <= maxRedirects; redirects += 1) {
        const controller = new AbortController();
        const timeout = setTimeout(() => controller.abort(), options.timeoutMs ?? 10_000);
        let response;
        try {
            response = await fetch(current, { ...init, redirect: 'manual', signal: controller.signal });
        }
        catch {
            throw new Error('Network request failed.');
        }
        finally {
            clearTimeout(timeout);
        }
        const contentLength = Number(response.headers.get('content-length') || '0');
        if (Number.isFinite(contentLength) && contentLength > (options.maxResponseBytes ?? 2_000_000)) {
            await response.body?.cancel();
            throw new Error('Network response exceeded the safety size limit.');
        }
        if (![301, 302, 303, 307, 308].includes(response.status)) {
            return withBoundResponseBody(response, options.maxResponseBytes ?? 2_000_000);
        }
        const location = response.headers.get('location');
        await response.body?.cancel();
        if (!location || redirects === maxRedirects) {
            throw new Error('Network redirect limit exceeded.');
        }
        current = await assertSafeNetworkUrl(new URL(location, current), redirectPolicy);
    }
    throw new Error('Network redirect limit exceeded.');
}
function withBoundResponseBody(response, maxResponseBytes) {
    if (!response.body)
        return response;
    let received = 0;
    const bounded = response.body.pipeThrough(new TransformStream({
        transform(chunk, controller) {
            received += chunk.byteLength;
            if (received > maxResponseBytes) {
                controller.error(new Error('Network response exceeded the safety size limit.'));
                return;
            }
            controller.enqueue(chunk);
        },
    }));
    return new Response(bounded, { status: response.status, statusText: response.statusText, headers: response.headers });
}
async function resolveHost(hostname, resolver) {
    try {
        if (resolver)
            return await resolver(hostname, { all: true, verbatim: true });
        return (await (0, promises_1.lookup)(hostname, { all: true, verbatim: true }));
    }
    catch {
        // A failed lookup is refused like a private answer, with the same text.
        return [];
    }
}
function isPermittedAddress(address, allowLoopback) {
    const family = node_net_1.default.isIP(address);
    if (family === 4)
        return isPermittedIpv4(address, allowLoopback);
    if (family === 6)
        return isPermittedIpv6(address, allowLoopback);
    return false;
}
function isExplicitLoopbackHostname(hostname) {
    const normalized = hostname.toLowerCase().replace(/\.$/, '');
    return normalized === 'localhost' || isLoopbackAddress(normalized);
}
function isLoopbackAddress(address) {
    const family = node_net_1.default.isIP(address);
    if (family === 4)
        return isLoopbackIpv4(address);
    if (family !== 6)
        return false;
    const hextets = parseIpv6Hextets(address);
    return Boolean(hextets && (isIpv6Loopback(hextets) || isLoopbackIpv4(ipv4EmbeddedInIpv6(hextets) ?? '')));
}
function isLoopbackIpv4(address) {
    return Number(address.split('.')[0]) === 127;
}
function isPermittedIpv4(address, allowLoopback) {
    const [first, second] = address.split('.').map(Number);
    if (first === 0 ||
        first >= 224 ||
        (first === 169 && second === 254) ||
        (first === 192 && second === 0) ||
        (first === 198 && (second === 18 || second === 19)) ||
        isDocumentationIpv4(address)) {
        return false;
    }
    const privateRange = first === 10 ||
        first === 127 ||
        (first === 100 && second >= 64 && second <= 127) ||
        (first === 172 && second >= 16 && second <= 31) ||
        (first === 192 && second === 168);
    return !privateRange || (allowLoopback && isLoopbackIpv4(address));
}
function isDocumentationIpv4(address) {
    return address.startsWith('192.0.2.') || address.startsWith('198.51.100.') || address.startsWith('203.0.113.');
}
function isPermittedIpv6(address, allowLoopback) {
    const hextets = parseIpv6Hextets(address);
    if (!hextets)
        return false;
    if (hextets.every((part) => part === 0))
        return false;
    if (isIpv6Loopback(hextets))
        return allowLoopback;
    if (isIpv6LinkLocal(hextets) ||
        isIpv6SiteLocal(hextets) ||
        (hextets[0] & 0xff00) === 0xff00 ||
        (hextets[0] === 0x2001 && hextets[1] === 0x0db8)) {
        return false;
    }
    if ((hextets[0] & 0xfe00) === 0xfc00)
        return false;
    // IPv4-mapped and IPv4-compatible literals can encode a special IPv4
    // destination. Apply the IPv4 policy after canonical parsing, regardless of
    // whether the original IPv6 text was compressed or expanded.
    const embeddedIpv4 = ipv4EmbeddedInIpv6(hextets);
    if (embeddedIpv4)
        return isPermittedIpv4(embeddedIpv4, allowLoopback);
    // The well-known NAT64 prefix can encode an IPv4 destination in its final
    // 32 bits. Apply the same policy before a resolver or network stack translates it.
    const nat64Ipv4 = wellKnownNat64Ipv4(hextets);
    if (nat64Ipv4)
        return isPermittedIpv4(nat64Ipv4, false);
    // 6to4 encodes its IPv4 gateway in the two hextets following 2002::/16.
    // Do not permit an IPv6 spelling to reach an internal IPv4 gateway.
    const sixToFourIpv4 = sixToFourIpv4Address(hextets);
    if (sixToFourIpv4)
        return isPermittedIpv4(sixToFourIpv4, false);
    return true;
}
function parseIpv6Hextets(address) {
    const parts = address.toLowerCase().split('::');
    if (parts.length > 2)
        return undefined;
    const left = parseIpv6Parts(parts[0]);
    const right = parts.length === 2 ? parseIpv6Parts(parts[1]) : [];
    if (!left || !right)
        return undefined;
    if (parts.length === 1)
        return left.length === 8 ? left : undefined;
    const omitted = 8 - left.length - right.length;
    return omitted >= 1 ? [...left, ...Array(omitted).fill(0), ...right] : undefined;
}
function parseIpv6Parts(value) {
    if (!value)
        return [];
    const parts = value.split(':');
    const hextets = [];
    for (const [index, part] of parts.entries()) {
        if (part.includes('.')) {
            if (index !== parts.length - 1)
                return undefined;
            const octets = part.split('.').map(Number);
            if (octets.length !== 4 || octets.some((octet) => !Number.isInteger(octet) || octet < 0 || octet > 255))
                return undefined;
            hextets.push((octets[0] << 8) | octets[1], (octets[2] << 8) | octets[3]);
            continue;
        }
        if (!/^[0-9a-f]{1,4}$/.test(part))
            return undefined;
        hextets.push(Number.parseInt(part, 16));
    }
    return hextets;
}
function isIpv6Loopback(hextets) {
    return hextets.slice(0, 7).every((part) => part === 0) && hextets[7] === 1;
}
function isIpv6LinkLocal(hextets) {
    return (hextets[0] & 0xffc0) === 0xfe80;
}
function isIpv6SiteLocal(hextets) {
    return (hextets[0] & 0xffc0) === 0xfec0;
}
function ipv4EmbeddedInIpv6(hextets) {
    const zeroPrefix = hextets.slice(0, 6).every((part) => part === 0);
    const mapped = hextets.slice(0, 5).every((part) => part === 0) && hextets[5] === 0xffff;
    return zeroPrefix || mapped ? ipv4FromHextets(hextets[6], hextets[7]) : undefined;
}
function wellKnownNat64Ipv4(hextets) {
    const wellKnownPrefix = hextets[0] === 0x0064 && hextets[1] === 0xff9b && hextets.slice(2, 6).every((part) => part === 0);
    return wellKnownPrefix ? ipv4FromHextets(hextets[6], hextets[7]) : undefined;
}
function sixToFourIpv4Address(hextets) {
    return hextets[0] === 0x2002 ? ipv4FromHextets(hextets[1], hextets[2]) : undefined;
}
function ipv4FromHextets(high, low) {
    return `${high >> 8}.${high & 0xff}.${low >> 8}.${low & 0xff}`;
}
