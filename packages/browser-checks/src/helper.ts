// Browser-check helper for the 9l binary. Go owns argument validation, the
// time budget, cancellation of this process tree and the size of what it
// reads back; this process owns the browser. One JSON request arrives on
// stdin and one JSON response leaves on stdout; nothing else is written there.
import { inspectPage, type InspectPageArgs } from './inspect-page';
import { type InspectResult, renderInspectReport, renderIssues, renderReport, type ScanResult } from './report';
import { scanUrl, type ScanUrlArgs } from './scan-url';
import type { Severity } from './common';
import { diagnose } from './doctor';

export const HELPER_PROTOCOL_VERSION = 1;

export type HelperRequest = {
  version: number;
  tool: 'scan_url' | 'inspect_page' | 'doctor';
  args: Record<string, unknown>;
  format?: 'json' | 'markdown' | 'issue';
  minSeverity?: Severity;
};

export type HelperResponse = { version: number; result?: unknown; text?: string; error?: string };

export async function handleRequest(request: HelperRequest): Promise<HelperResponse> {
  if (request?.version !== HELPER_PROTOCOL_VERSION) {
    return { version: HELPER_PROTOCOL_VERSION, error: 'unsupported browser-check request version' };
  }
  try {
    if (request.tool === 'doctor') return { version: HELPER_PROTOCOL_VERSION, result: diagnose(request.args) };
    if (request.tool === 'scan_url') {
      const result = (await scanUrl(request.args as ScanUrlArgs)) as ScanResult;
      if (request.format === 'issue') return { version: HELPER_PROTOCOL_VERSION, result, text: renderIssues(result, { minSeverity: request.minSeverity }) };
      if (request.format === 'markdown') return { version: HELPER_PROTOCOL_VERSION, result, text: renderReport(result) };
      return { version: HELPER_PROTOCOL_VERSION, result };
    }
    if (request.tool === 'inspect_page') {
      const result = (await inspectPage(request.args as InspectPageArgs)) as InspectResult;
      if (request.format === 'markdown') return { version: HELPER_PROTOCOL_VERSION, result, text: renderInspectReport(result) };
      return { version: HELPER_PROTOCOL_VERSION, result };
    }
    return { version: HELPER_PROTOCOL_VERSION, error: 'unknown browser check' };
  } catch (error) {
    return { version: HELPER_PROTOCOL_VERSION, error: error instanceof Error ? error.message : String(error) };
  }
}

async function readStdin(limit: number): Promise<string> {
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of process.stdin) {
    size += (chunk as Buffer).length;
    if (size > limit) throw new Error('browser-check request exceeds its size limit');
    chunks.push(chunk as Buffer);
  }
  return Buffer.concat(chunks).toString('utf8');
}

async function main(): Promise<void> {
  let response: HelperResponse;
  try {
    response = await handleRequest(JSON.parse(await readStdin(16 << 20)) as HelperRequest);
  } catch (error) {
    response = { version: HELPER_PROTOCOL_VERSION, error: error instanceof Error ? error.message : 'invalid browser-check request' };
  }
  // Exit once the response is flushed: a page can leave timers or sockets
  // behind that would otherwise keep the process alive after the browser closed.
  process.stdout.write(JSON.stringify(response) + '\n', () => process.exit(response.error ? 1 : 0));
}

if (require.main === module) void main();
