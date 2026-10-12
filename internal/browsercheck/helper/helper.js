"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.HELPER_PROTOCOL_VERSION = void 0;
exports.handleRequest = handleRequest;
// Browser-check helper for the 9l binary. Go owns argument validation, the
// time budget, cancellation of this process tree and the size of what it
// reads back; this process owns the browser. One JSON request arrives on
// stdin and one JSON response leaves on stdout; nothing else is written there.
const inspect_page_1 = require("./inspect-page");
const report_1 = require("./report");
const scan_url_1 = require("./scan-url");
exports.HELPER_PROTOCOL_VERSION = 1;
async function handleRequest(request) {
    if (request?.version !== exports.HELPER_PROTOCOL_VERSION) {
        return { version: exports.HELPER_PROTOCOL_VERSION, error: 'unsupported browser-check request version' };
    }
    try {
        if (request.tool === 'scan_url') {
            const result = (await (0, scan_url_1.scanUrl)(request.args));
            if (request.format === 'issue')
                return { version: exports.HELPER_PROTOCOL_VERSION, result, text: (0, report_1.renderIssues)(result, { minSeverity: request.minSeverity }) };
            if (request.format === 'markdown')
                return { version: exports.HELPER_PROTOCOL_VERSION, result, text: (0, report_1.renderReport)(result) };
            return { version: exports.HELPER_PROTOCOL_VERSION, result };
        }
        if (request.tool === 'inspect_page') {
            const result = (await (0, inspect_page_1.inspectPage)(request.args));
            if (request.format === 'markdown')
                return { version: exports.HELPER_PROTOCOL_VERSION, result, text: (0, report_1.renderInspectReport)(result) };
            return { version: exports.HELPER_PROTOCOL_VERSION, result };
        }
        return { version: exports.HELPER_PROTOCOL_VERSION, error: 'unknown browser check' };
    }
    catch (error) {
        return { version: exports.HELPER_PROTOCOL_VERSION, error: error instanceof Error ? error.message : String(error) };
    }
}
async function readStdin(limit) {
    const chunks = [];
    let size = 0;
    for await (const chunk of process.stdin) {
        size += chunk.length;
        if (size > limit)
            throw new Error('browser-check request exceeds its size limit');
        chunks.push(chunk);
    }
    return Buffer.concat(chunks).toString('utf8');
}
async function main() {
    let response;
    try {
        response = await handleRequest(JSON.parse(await readStdin(16 << 20)));
    }
    catch (error) {
        response = { version: exports.HELPER_PROTOCOL_VERSION, error: error instanceof Error ? error.message : 'invalid browser-check request' };
    }
    // Exit once the response is flushed: a page can leave timers or sockets
    // behind that would otherwise keep the process alive after the browser closed.
    process.stdout.write(JSON.stringify(response) + '\n', () => process.exit(response.error ? 1 : 0));
}
if (require.main === module)
    void main();
