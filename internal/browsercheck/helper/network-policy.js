"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.assertSafeNetworkUrl = assertSafeNetworkUrl;
exports.bareHostname = bareHostname;
exports.pinnedLookup = pinnedLookup;
exports.safeUrlForDisplay = safeUrlForDisplay;
exports.safeFetch = safeFetch;
const promises_1 = require("node:dns/promises");
const node_http_1 = __importDefault(require("node:http"));
const node_https_1 = __importDefault(require("node:https"));
const node_net_1 = __importDefault(require("node:net"));
const node_stream_1 = require("node:stream");
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
    const hostname = bareHostname(url);
    const addresses = node_net_1.default.isIP(hostname)
        ? [{ address: hostname, family: node_net_1.default.isIP(hostname) }]
        : await resolveHost(hostname, options.lookup);
    assertPermittedAddresses(url, hostname, addresses, options);
    return url;
}
/** A refusal by the policy, kept apart from transport failures so its text reaches the caller. */
class NetworkPolicyRefusal extends Error {
}
function bareHostname(url) {
    return url.hostname.replace(/^\[|\]$/g, '');
}
/** Refuse unless every answer is permitted for this URL; an empty answer is refused too. */
function assertPermittedAddresses(url, hostname, addresses, options) {
    const loopbackAccessApproved = Boolean(options.allowPrivateNetwork) &&
        (!options.privateNetworkOrigin || url.origin === options.privateNetworkOrigin) &&
        isExplicitLoopbackHostname(hostname);
    if (!addresses.length || addresses.some(({ address }) => !isPermittedAddress(address, loopbackAccessApproved))) {
        throw new NetworkPolicyRefusal(rejectionFor(url, hostname, options));
    }
}
/**
 * A `lookup` for the socket that applies the policy to the answers it will
 * connect to.
 *
 * `assertSafeNetworkUrl` checks the answer it sees, but a socket resolves the
 * name again when it connects, and a hostname whose DNS answer changes in
 * between (DNS rebinding) could reach an address the check would have refused.
 * Resolving here, through the same resolver, and refusing unless every answer
 * is permitted means the socket can only connect to an address that passed the
 * policy. The refusal is the check's own text, so it names no address and a
 * failed lookup reads the same as a private answer. Address literals are not
 * looked up by the socket; the check already covered them.
 */
function pinnedLookup(url, options) {
    const hostname = bareHostname(url);
    return (host, lookupOptions, callback) => {
        void (async () => {
            const answers = await resolveHost(host, options.lookup);
            try {
                assertPermittedAddresses(url, hostname, answers, options);
            }
            catch (error) {
                callback(error, '', 0);
                return;
            }
            const family = lookupFamily(lookupOptions.family);
            const candidates = family ? answers.filter((answer) => answer.family === family) : answers;
            if (!candidates.length) {
                callback(Object.assign(new Error('No address of the requested family.'), { code: 'ENOTFOUND' }), '', 0);
                return;
            }
            if (lookupOptions.all)
                callback(null, candidates);
            else
                callback(null, candidates[0].address, candidates[0].family);
        })();
    };
}
function lookupFamily(family) {
    if (family === 4 || family === 'IPv4')
        return 4;
    if (family === 6 || family === 'IPv6')
        return 6;
    return 0;
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
/**
 * Request a URL under the network policy, for checks that run in Node rather
 * than in the browser (link checks).
 *
 * Each hop is checked before the request and again when its socket connects
 * (see `pinnedLookup`), redirects are followed by hand so every target is
 * checked, and loopback consent never extends past the first approved origin.
 * The response is a `Response` with the status, headers and a body bounded by
 * `maxResponseBytes`.
 */
async function safeFetch(input, init = {}, options = {}) {
    let current = await assertSafeNetworkUrl(input, options);
    const redirectPolicy = options.allowPrivateNetwork && !options.privateNetworkOrigin
        ? { ...options, privateNetworkOrigin: current.origin }
        : options;
    const maxRedirects = options.maxRedirects ?? 5;
    const maxResponseBytes = options.maxResponseBytes ?? 2_000_000;
    for (let redirects = 0; redirects <= maxRedirects; redirects += 1) {
        const message = await pinnedRequest(current, init, redirects === 0 ? options : redirectPolicy, options.timeoutMs ?? 10_000);
        const contentLength = Number(message.headers['content-length'] || '0');
        if (Number.isFinite(contentLength) && contentLength > maxResponseBytes) {
            message.destroy();
            throw new Error('Network response exceeded the safety size limit.');
        }
        if (![301, 302, 303, 307, 308].includes(message.statusCode ?? 0)) {
            return toBoundedResponse(message, init.method, maxResponseBytes);
        }
        const location = message.headers.location;
        message.destroy();
        if (!location || redirects === maxRedirects) {
            throw new Error('Network redirect limit exceeded.');
        }
        current = await assertSafeNetworkUrl(new URL(location, current), redirectPolicy);
    }
    throw new Error('Network redirect limit exceeded.');
}
// What Node's fetch sent by default, so servers answer link checks as before.
// No Accept-Encoding: the body is read as sent.
const DEFAULT_REQUEST_HEADERS = { accept: '*/*', 'accept-language': '*', 'user-agent': 'node' };
/**
 * One request over a fresh connection whose address passed the policy. The
 * timeout covers the lookup, the connection and the headers, as before, and
 * then keeps running, unreferenced, until the body ends, so an unread body
 * cannot hold a socket open beyond it. Transport failures read as one generic
 * error; a policy refusal keeps its own text.
 *
 * The promise settles exactly once. A `101 Switching Protocols` answer (or a
 * reply to CONNECT) emits neither `response` nor `error`, and destroying the
 * request afterwards emits nothing either, so without its own handler such an
 * answer left the check pending past its timeout. It is refused as a transport
 * failure, and `close` settles anything else that ends without a response.
 */
function pinnedRequest(url, init, policy, timeoutMs) {
    return new Promise((resolve, reject) => {
        let settled = false;
        const fail = (error) => {
            if (settled)
                return;
            settled = true;
            clearTimeout(timeout);
            reject(error instanceof NetworkPolicyRefusal ? new Error(error.message) : new Error('Network request failed.'));
        };
        const request = (url.protocol === 'https:' ? node_https_1.default : node_http_1.default).request({
            protocol: url.protocol,
            hostname: bareHostname(url),
            port: url.port || undefined,
            path: `${url.pathname}${url.search}`,
            method: init.method ?? 'GET',
            headers: { ...DEFAULT_REQUEST_HEADERS, ...init.headers },
            // A fresh agent: a pooled socket may have been connected without this lookup.
            agent: false,
            lookup: pinnedLookup(url, policy),
        });
        const timeout = setTimeout(() => {
            request.destroy();
            fail();
        }, timeoutMs);
        // Kept for the request's lifetime: an error after the response must not go uncaught.
        request.on('error', fail);
        // Registering these takes the upgraded socket from Node; nothing here speaks another protocol.
        const refuseUpgrade = (_message, socket) => {
            socket.destroy();
            fail();
        };
        request.once('upgrade', refuseUpgrade);
        request.once('connect', refuseUpgrade);
        request.once('close', () => fail());
        request.once('response', (message) => {
            if (settled) {
                message.destroy();
                return;
            }
            settled = true;
            timeout.unref();
            message.once('close', () => clearTimeout(timeout));
            resolve(message);
        });
        request.end();
    });
}
function toBoundedResponse(message, method, maxResponseBytes) {
    const status = message.statusCode ?? 0;
    const headers = new Headers();
    for (let index = 0; index + 1 < message.rawHeaders.length; index += 2) {
        try {
            headers.append(message.rawHeaders[index], message.rawHeaders[index + 1]);
        }
        catch {
            // A header value fetch would not accept is left out rather than failing the check.
        }
    }
    const nullBody = (method ?? 'GET').toUpperCase() === 'HEAD' || [204, 205, 304].includes(status);
    try {
        if (nullBody) {
            message.resume();
            return new Response(null, { status, statusText: message.statusMessage, headers });
        }
        const body = node_stream_1.Readable.toWeb(message);
        return new Response(boundedBody(body, maxResponseBytes), { status, statusText: message.statusMessage, headers });
    }
    catch {
        // A status outside 200-599 or an invalid status text, which fetch also refuses.
        message.destroy();
        throw new Error('Network request failed.');
    }
}
function boundedBody(body, maxResponseBytes) {
    let received = 0;
    return body.pipeThrough(new TransformStream({
        transform(chunk, controller) {
            received += chunk.byteLength;
            if (received > maxResponseBytes) {
                controller.error(new Error('Network response exceeded the safety size limit.'));
                return;
            }
            controller.enqueue(chunk);
        },
    }));
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
