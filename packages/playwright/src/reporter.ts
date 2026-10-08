import {createHash} from 'node:crypto';
import {closeSync, openSync, writeSync} from 'node:fs';
import type {Reporter, FullConfig, Suite, TestCase, TestResult, TestStep, FullResult} from '@playwright/test/reporter';
import {capabilities, engineEventsPath, engineIdentity, maxEvents, maxFrameBytes, skipPinKey} from './protocol';

// Worker logs, error messages, step titles, URLs and attachment bodies never enter
// the protocol. IDs are hashes; Go persists only this bounded metadata stream.
export default class EngineReporter implements Reporter {
  private identity = engineIdentity();
  // Exclusive create: the file must be new for this attempt, never appended to.
  private events = openSync(engineEventsPath(), 'wx', 0o600);
  private seq = 0;
  private stepSeq = 0;
  private suite?: Suite;
  private terminal = false;
  private overflow = false;

  printsToStdio() { return true; }
  private testId(test: TestCase) { return createHash('sha256').update(test.id).digest('hex'); }
  private emit(type: string, fields: Record<string, unknown> = {}) {
    if (this.terminal || this.overflow) return;
    if (this.seq >= maxEvents - 1) {
      this.overflow = true;
      writeSync(this.events, JSON.stringify({...this.identity, seq: ++this.seq, type: 'engine_error'}) + '\n');
      return;
    }
    const frame = JSON.stringify({...this.identity, seq: ++this.seq, type, ...fields}) + '\n';
    if (Buffer.byteLength(frame) > maxFrameBytes) {
      this.overflow = true;
      writeSync(this.events, JSON.stringify({...this.identity, seq: this.seq, type: 'engine_error'}) + '\n');
      return;
    }
    writeSync(this.events, frame);
  }
  onBegin(_config: FullConfig, suite: Suite) {
    this.suite = suite;
    this.emit('hello', {capabilities, totalTests: suite.allTests().length});
  }
  onTestBegin(test: TestCase, result: TestResult) {
    this.emit('test_begin', {testId: this.testId(test), retry: result.retry});
  }
  onStepEnd(test: TestCase, result: TestResult, step: TestStep) {
    const categories: Record<string, string> = {'test.step': 'action', expect: 'assertion', 'pw:api': 'browser', fixture: 'fixture'};
    this.emit('step_end', {
      testId: this.testId(test), retry: result.retry, stepId: `step-${++this.stepSeq}`,
      category: step.category === 'test.step' && step.title === '9l goal' ? 'goal' : categories[step.category] ?? 'other', status: step.error ? 'failed' : 'passed',
    });
  }
  onTestEnd(test: TestCase, result: TestResult) {
    if (result.attachments.length > 32) { this.emit('engine_error'); return; }
    this.emit('test_end', {
      testId: this.testId(test), retry: result.retry, status: result.status, expectedStatus: test.expectedStatus,
      artifacts: result.attachments.map((attachment, index) => ({
        id: `artifact-${index + 1}`,
        kind: attachment.contentType.startsWith('image/') ? 'image' : attachment.contentType === 'application/json' ? 'json' : 'other',
        retained: false,
      })),
    });
  }
  onError() { this.emit('engine_error'); }
  onStdOut() {} // Do not mix test logs with evidence or retain arbitrary payloads.
  onStdErr() {}
  onEnd(result: FullResult) {
    // Individual outcomes are separate bounded frames, even for large suites.
    for (const test of this.suite?.allTests() ?? []) {
      const outcome = test.outcome();
      this.emit('test_result', {testId: this.testId(test), outcome, ...(outcome === 'skipped' ? {skipKey: skipPinKey(test.titlePath())} : {})});
    }
    this.emit('end', {status: result.status});
    this.terminal = true;
    closeSync(this.events);
  }
}
