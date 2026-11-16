import http, { type IncomingHttpHeaders, type IncomingMessage, type ServerResponse } from 'node:http';
import net from 'node:net';
import type { Duplex } from 'node:stream';
import { assertSafeNetworkUrl, bareHostname, pinnedLookup, type NetworkPolicyOptions } from './network-policy';

/**
 * A local HTTP proxy that applies the network policy to every connection the
 * browser makes (#108).
 *
 * The browser context's route handler checks each request's first URL, but
 * Chromium follows redirects inside its network stack without calling the
 * handler again, and connects to whatever the name resolves to when it
 * connects. A navigation, subresource or in-page fetch that redirected to a
 * private or another loopback address was therefore requested, and a hostname
 * whose DNS answer changed after the check (DNS rebinding) could reach an
 * address the check would have refused.
 *
 * Routing the browser through this proxy means every hop of every request,
 * redirects included, arrives here as its own request. Plain HTTP requests are
 * forwarded, and HTTPS and WebSocket traffic is tunnelled with CONNECT, over
 * sockets whose `lookup` refuses unless every answer passes the policy, so the
 * address connected to is the address checked. Address literals are never
 * looked up and are checked before connecting. A refused or failed request
 * ends as a network failure in the browser; no page content is substituted.
 */
export type PolicyProxy = {
  /** The proxy address for the browser's `proxy.server` launch option. */
  server: string;
  close(): Promise<void>;
};

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

export async function startPolicyProxy(policy: NetworkPolicyOptions): Promise<PolicyProxy> {
  const sockets = new Set<net.Socket | Duplex>();
  const track = <T extends net.Socket | Duplex>(socket: T): T => {
    sockets.add(socket);
    socket.once('close', () => sockets.delete(socket));
    return socket;
  };
  // Connections to origins are reused only for the same host and port, which
  // passed the same policy when the socket connected.
  const agent = new http.Agent({ keepAlive: true });

  const server = http.createServer((request, response) => {
    void forward(request, response, policy, agent);
  });
  server.on('connection', (socket) => track(socket));
  server.on('connect', (request: IncomingMessage, socket: Duplex, head: Buffer) => {
    void tunnel(request, track(socket), head, policy, track);
  });
  // Nothing here speaks another protocol over a plain HTTP request.
  server.on('upgrade', (_request: IncomingMessage, socket: Duplex) => socket.destroy());
  server.on('clientError', (_error, socket) => socket.destroy());

  await new Promise<void>((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => {
      server.off('error', reject);
      resolve();
    });
  });
  const { port } = server.address() as net.AddressInfo;

  return {
    server: `http://127.0.0.1:${port}`,
    async close() {
      const closed = new Promise<void>((resolve) => server.close(() => resolve()));
      for (const socket of sockets) socket.destroy();
      agent.destroy();
      await closed;
    },
  };
}

/** Check an address literal before connecting; a name is checked by `pinnedLookup` when the socket connects. */
async function assertLiteralPermitted(url: URL, policy: NetworkPolicyOptions): Promise<void> {
  if (net.isIP(bareHostname(url))) await assertSafeNetworkUrl(url, policy);
}

async function forward(request: IncomingMessage, response: ServerResponse, policy: NetworkPolicyOptions, agent: http.Agent): Promise<void> {
  // Ends the browser's request as a network failure rather than an answer the page could read.
  const fail = () => {
    response.socket?.destroy();
    response.destroy();
  };
  let url: URL;
  try {
    url = new URL(request.url ?? '');
    if (url.protocol !== 'http:') throw new Error('not an absolute http URL');
    await assertLiteralPermitted(url, policy);
  } catch {
    fail();
    return;
  }

  const upstream = http.request({
    protocol: 'http:',
    hostname: bareHostname(url),
    port: url.port || 80,
    path: `${url.pathname}${url.search}`,
    method: request.method,
    headers: endToEndRawHeaders(request.headers, request.rawHeaders),
    agent,
    lookup: pinnedLookup(url, policy),
  });
  upstream.on('error', fail);
  request.on('error', () => upstream.destroy());
  response.on('close', () => {
    if (!response.writableFinished) upstream.destroy();
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

async function tunnel(
  request: IncomingMessage,
  client: Duplex,
  head: Buffer,
  policy: NetworkPolicyOptions,
  track: <T extends net.Socket | Duplex>(socket: T) => T
): Promise<void> {
  client.on('error', () => client.destroy());
  let url: URL;
  let port: number;
  try {
    // CONNECT names only a host and port; https is the origin the browser asks for there.
    // The port is read here because the URL drops a default one (443).
    const authority = /^(\[[0-9a-fA-F:.]+\]|[^:/?#@[\]\s]+):(\d{1,5})$/.exec(request.url ?? '');
    port = Number(authority?.[2]);
    if (!authority || port < 1 || port > 65_535) throw new Error('not host:port');
    url = new URL(`https://${authority[1]}:${port}`);
    await assertLiteralPermitted(url, policy);
  } catch {
    client.end('HTTP/1.1 403 Forbidden\r\nconnection: close\r\ncontent-length: 0\r\n\r\n');
    return;
  }

  const upstream = track(
    net.connect({
      host: bareHostname(url),
      port,
      lookup: pinnedLookup(url, policy),
      timeout: CONNECT_TIMEOUT_MS,
    })
  );
  let connected = false;
  upstream.once('timeout', () => upstream.destroy());
  upstream.once('connect', () => {
    connected = true;
    upstream.setTimeout(0);
    client.write('HTTP/1.1 200 Connection Established\r\n\r\n');
    if (head.length) upstream.write(head);
    upstream.pipe(client);
    client.pipe(upstream);
  });
  upstream.on('error', () => {
    if (connected) client.destroy();
    else client.end('HTTP/1.1 502 Bad Gateway\r\nconnection: close\r\ncontent-length: 0\r\n\r\n');
  });
  upstream.once('close', () => client.destroy());
  client.once('close', () => upstream.destroy());
}

/** Header names the `Connection` header lists, which are hop-by-hop too. */
function connectionTokens(headers: IncomingHttpHeaders): Set<string> {
  const value = headers.connection;
  const tokens = Array.isArray(value) ? value.join(',') : value ?? '';
  return new Set(tokens.split(',').map((token) => token.trim().toLowerCase()).filter(Boolean));
}

function endToEndRawHeaders(headers: IncomingHttpHeaders, rawHeaders: string[]): string[] {
  const listed = connectionTokens(headers);
  const kept: string[] = [];
  for (let index = 0; index + 1 < rawHeaders.length; index += 2) {
    const name = rawHeaders[index].toLowerCase();
    if (HOP_BY_HOP.has(name) || listed.has(name)) continue;
    kept.push(rawHeaders[index], rawHeaders[index + 1]);
  }
  return kept;
}
