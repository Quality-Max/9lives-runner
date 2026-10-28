"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.startPolicyProxy = startPolicyProxy;
const node_http_1 = __importDefault(require("node:http"));
const node_net_1 = __importDefault(require("node:net"));
const network_policy_1 = require("./network-policy");
const CONNECT_TIMEOUT_MS = 30_000;
// Hop-by-hop headers (RFC 9110 section 7.6.1) and the proxy's own; never forwarded.
const HOP_BY_HOP = new Set([
    'connection',
    'keep-alive',
    'proxy-authenticate',
    'proxy-authorization',
    'proxy-connection',
    'te',
    'trailer',
    'transfer-encoding',
    'upgrade',
]);
async function startPolicyProxy(policy) {
    const sockets = new Set();
    const track = (socket) => {
        sockets.add(socket);
        socket.once('close', () => sockets.delete(socket));
        return socket;
    };
    // Connections to origins are reused only for the same host and port, which
    // passed the same policy when the socket connected.
    const agent = new node_http_1.default.Agent({ keepAlive: true });
    const server = node_http_1.default.createServer((request, response) => {
        void forward(request, response, policy, agent);
    });
    server.on('connection', (socket) => track(socket));
    server.on('connect', (request, socket, head) => {
        void tunnel(request, track(socket), head, policy, track);
    });
    // Nothing here speaks another protocol over a plain HTTP request.
    server.on('upgrade', (_request, socket) => socket.destroy());
    server.on('clientError', (_error, socket) => socket.destroy());
    await new Promise((resolve, reject) => {
        server.once('error', reject);
        server.listen(0, '127.0.0.1', () => {
            server.off('error', reject);
            resolve();
        });
    });
    const { port } = server.address();
    return {
        server: `http://127.0.0.1:${port}`,
        async close() {
            const closed = new Promise((resolve) => server.close(() => resolve()));
            for (const socket of sockets)
                socket.destroy();
            agent.destroy();
            await closed;
        },
    };
}
/** Check an address literal before connecting; a name is checked by `pinnedLookup` when the socket connects. */
async function assertLiteralPermitted(url, policy) {
    if (node_net_1.default.isIP((0, network_policy_1.bareHostname)(url)))
        await (0, network_policy_1.assertSafeNetworkUrl)(url, policy);
}
async function forward(request, response, policy, agent) {
    // Ends the browser's request as a network failure rather than an answer the page could read.
    const fail = () => {
        response.socket?.destroy();
        response.destroy();
    };
    let url;
    try {
        url = new URL(request.url ?? '');
        if (url.protocol !== 'http:')
            throw new Error('not an absolute http URL');
        await assertLiteralPermitted(url, policy);
    }
    catch {
        fail();
        return;
    }
    const upstream = node_http_1.default.request({
        protocol: 'http:',
        hostname: (0, network_policy_1.bareHostname)(url),
        port: url.port || 80,
        path: `${url.pathname}${url.search}`,
        method: request.method,
        headers: endToEndRawHeaders(request.headers, request.rawHeaders),
        agent,
        lookup: (0, network_policy_1.pinnedLookup)(url, policy),
    });
    upstream.on('error', fail);
    request.on('error', () => upstream.destroy());
    response.on('close', () => {
        if (!response.writableFinished)
            upstream.destroy();
    });
    upstream.once('upgrade', (_message, socket) => {
        socket.destroy();
        fail();
    });
    upstream.once('response', (message) => {
        response.writeHead(message.statusCode ?? 502, message.statusMessage, endToEndRawHeaders(message.headers, message.rawHeaders));
        message.pipe(response);
        message.on('error', fail);
    });
    request.pipe(upstream);
}
async function tunnel(request, client, head, policy, track) {
    client.on('error', () => client.destroy());
    let url;
    let port;
    try {
        // CONNECT names only a host and port; https is the origin the browser asks for there.
        // The port is read here because the URL drops a default one (443).
        const authority = /^(\[[0-9a-fA-F:.]+\]|[^:/?#@[\]\s]+):(\d{1,5})$/.exec(request.url ?? '');
        port = Number(authority?.[2]);
        if (!authority || port < 1 || port > 65_535)
            throw new Error('not host:port');
        url = new URL(`https://${authority[1]}:${port}`);
        await assertLiteralPermitted(url, policy);
    }
    catch {
        client.end('HTTP/1.1 403 Forbidden\r\nconnection: close\r\ncontent-length: 0\r\n\r\n');
        return;
    }
    const upstream = track(node_net_1.default.connect({
        host: (0, network_policy_1.bareHostname)(url),
        port,
        lookup: (0, network_policy_1.pinnedLookup)(url, policy),
        timeout: CONNECT_TIMEOUT_MS,
    }));
    let connected = false;
    upstream.once('timeout', () => upstream.destroy());
    upstream.once('connect', () => {
        connected = true;
        upstream.setTimeout(0);
        client.write('HTTP/1.1 200 Connection Established\r\n\r\n');
        if (head.length)
            upstream.write(head);
        upstream.pipe(client);
        client.pipe(upstream);
    });
    upstream.on('error', () => {
        if (connected)
            client.destroy();
        else
            client.end('HTTP/1.1 502 Bad Gateway\r\nconnection: close\r\ncontent-length: 0\r\n\r\n');
    });
    upstream.once('close', () => client.destroy());
    client.once('close', () => upstream.destroy());
}
/** Header names the `Connection` header lists, which are hop-by-hop too. */
function connectionTokens(headers) {
    const value = headers.connection;
    const tokens = Array.isArray(value) ? value.join(',') : value ?? '';
    return new Set(tokens.split(',').map((token) => token.trim().toLowerCase()).filter(Boolean));
}
function endToEndRawHeaders(headers, rawHeaders) {
    const listed = connectionTokens(headers);
    const kept = [];
    for (let index = 0; index + 1 < rawHeaders.length; index += 2) {
        const name = rawHeaders[index].toLowerCase();
        if (HOP_BY_HOP.has(name) || listed.has(name))
            continue;
        kept.push(rawHeaders[index], rawHeaders[index + 1]);
    }
    return kept;
}
