import {createHash} from 'node:crypto';
import {closeSync, openSync, writeSync} from 'node:fs';
import type {Reporter, FullConfig, Suite, TestCase, TestError, TestResult, TestStep, FullResult} from '@playwright/test/reporter';
import {capabilities, engineEventsPath, engineIdentity, maxEvents, maxFrameBytes, skipPinKey, selectedTarget, targetStepFile, targetVersion} from './protocol';
import {validIOSRuntime, iosRuntimeType} from './ios-runtime';
import {validAndroidRuntime, androidRuntimeType} from './android-runtime';
import {validTargetSetup, targetSetupType, validTargetDiagnostic, targetDiagnosticType} from './target-setup';

function insideAssertion(step: TestStep) {
  for (let ancestor = step.parent; ancestor; ancestor = ancestor.parent) {
    if (ancestor.category === 'expect') return true;
  }
  return false;
}

// A typed target step fails only with "9lives target stopped: <reason>", where
// the reason is the fixture's own token, optionally followed by the one fixed
// detail the fixture writes (the accepted range of an invalid timeout). Only
// the token crosses the bridge; any other error text, or none, sends no reason.
function targetStopReason(error: TestError | undefined) {
  return /^(?:Error: )?9lives target stopped: ([a-z_]{1,64})(?: \(accepted 1-[0-9]{1,7} ms\))?$/.exec(error?.message ?? '')?.[1];
}

// Worker logs, error messages, step titles, URLs and attachment bodies never enter
// the protocol. IDs are hashes; Go persists only this bounded metadata stream.
export default class EngineReporter implements Reporter {
  private identity = engineIdentity();
  private target = process.env.NINELIVES_TARGET ? selectedTarget() : undefined;
  // Exclusive create: the file must be new for this attempt, never appended to.
  private events = openSync(engineEventsPath(), 'wx', 0o600);
  private seq = 0;
  private stepSeq = 0;
  private suite?: Suite;
  // The expected status of each test before any attempt ran. Playwright
  // updates test.expectedStatus only when an attempt ends, so a test_begin
  // frame carries the declaration (test.fail('title', ...) or a suite-level
  // modifier) and test_end the final value; Go reads a difference as a
  // modifier that ran during the attempt, in the body or in an after hook.
  private declared = new Map<string, string>();
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
    const frame = JSON.stringify({...this.identity, ...(this.target ? {targetId: this.target.id} : {}), seq: ++this.seq, type, ...fields}) + '\n';
    if (Buffer.byteLength(frame) > maxFrameBytes) {
      this.overflow = true;
      writeSync(this.events, JSON.stringify({...this.identity, seq: this.seq, type: 'engine_error'}) + '\n');
      return;
    }
    writeSync(this.events, frame);
  }
  onBegin(_config: FullConfig, suite: Suite) {
    this.suite = suite;
    for (const test of suite.allTests()) this.declared.set(test.id, test.expectedStatus);
    this.emit('hello', {capabilities: this.target ? [...capabilities, targetVersion] : capabilities, totalTests: suite.allTests().length, ...(this.target ? {targetVersion} : {})});
    if (this.target) {
      for (const test of suite.allTests()) {
        if (test.parent.project()?.name !== this.target.name) { this.emit('engine_error'); return; }
        this.emit('required_test', {testId: this.testId(test)});
      }
    }
  }
  onTestBegin(test: TestCase, result: TestResult) {
    this.emit('test_begin', {testId: this.testId(test), retry: result.retry, expectedStatus: this.declared.get(test.id) ?? test.expectedStatus});
  }
  onStepEnd(test: TestCase, result: TestResult, step: TestStep) {
    // expect.poll, toPass and custom matchers report the assertions they run,
    // on every retry, as expect steps beneath their own, sometimes inside a
    // test.step. A retrying assertion counts once, including the assertions
    // inside it; its own step carries the final outcome.
    if (step.category === 'expect' && insideAssertion(step)) return;
    const categories: Record<string, string> = {'test.step': 'action', expect: 'assertion', 'pw:api': 'browser', fixture: 'fixture'};
    const typed = this.target && step.category === 'test.step' && step.location?.file === targetStepFile && step.title.startsWith('9l target ') ? step.title.slice('9l target '.length) : undefined;
    // "<operation>" or "<operation> <control>": the control is the test ID
    // the fixture named, an identifier only. Anything else keeps the whole
    // remainder as the operation, which the engine rejects as unknown. Only a
    // failed step names its control, so passing evidence carries no test ID.
    const named = typed === undefined ? undefined : /^([a-z][a-z-]{0,63}) ([A-Za-z0-9_][A-Za-z0-9_.-]{0,63})$/.exec(typed);
    const operation = named ? named[1] : typed, control = step.error ? named?.[2] : undefined;
    const reason = operation && step.error ? targetStopReason(step.error) : undefined;
    this.emit('step_end', {
      testId: this.testId(test), retry: result.retry, stepId: `step-${++this.stepSeq}`,
      category: operation ? operation.startsWith('assert-') ? 'assertion' : 'action' : step.category === 'test.step' && step.title === '9l goal' ? 'goal' : categories[step.category] ?? 'other', status: step.error ? 'failed' : 'passed',
      ...(operation ? {operation} : {}),
      ...(control ? {control} : {}),
      ...(reason ? {reason} : {}),
    });
  }
  onTestEnd(test: TestCase, result: TestResult) {
    if (result.attachments.length > 32) { this.emit('engine_error'); return; }
    // Bounded classifications from a native target only: at most one made
    // before the test ran (a recovered baseline) and one from a recheck.
    const setups = result.attachments.filter(attachment => attachment.name === '9l.target-setup');
    const stages = setups.map(attachment => { try { return JSON.parse(attachment.body?.toString('utf8') ?? '')?.stage; } catch { return undefined; } });
    if (result.attachments.filter(attachment => attachment.name === '9l.target-diagnostic').length > 1 || setups.length > 2 || (setups.length === 2 && !(stages[0] === 'open' && stages[1] === 'recheck'))) { this.emit('engine_error'); return; }
    for (const attachment of result.attachments) {
      if (attachment.name === '9l.target-diagnostic') {
        if (!this.target || this.target.kind === 'web' || attachment.contentType !== targetDiagnosticType || !attachment.body || attachment.body.length > 1024) { this.emit('engine_error'); return; }
        let diagnostic: unknown;
        try { diagnostic = JSON.parse(attachment.body.toString('utf8')); } catch { this.emit('engine_error'); return; }
        if (!validTargetDiagnostic(diagnostic)) { this.emit('engine_error'); return; }
        this.emit('target_diagnostic', {testId: this.testId(test), retry: result.retry, diagnostic});
        continue;
      }
      if (attachment.name === '9l.target-setup') {
        if (!this.target || this.target.kind === 'web' || attachment.contentType !== targetSetupType || !attachment.body || attachment.body.length > 512) { this.emit('engine_error'); return; }
        let setup: unknown;
        try { setup = JSON.parse(attachment.body.toString('utf8')); } catch { this.emit('engine_error'); return; }
        if (!validTargetSetup(setup)) { this.emit('engine_error'); return; }
        this.emit('target_setup', {testId: this.testId(test), retry: result.retry, setup});
        continue;
      }
      if (attachment.name === '9l.ios-runtime') {
        if (this.target?.kind !== 'ios' || attachment.contentType !== iosRuntimeType || !attachment.body || attachment.body.length > 2048) { this.emit('engine_error'); return; }
        let iosRuntime: unknown;
        try { iosRuntime = JSON.parse(attachment.body.toString('utf8')); } catch { this.emit('engine_error'); return; }
        if (!validIOSRuntime(iosRuntime)) { this.emit('engine_error'); return; }
        this.emit('ios_runtime', {testId: this.testId(test), retry: result.retry, iosRuntime});
        continue;
      }
      if (attachment.name !== '9l.android-runtime') continue;
      if (this.target?.kind !== 'android' || attachment.contentType !== androidRuntimeType || !attachment.body || attachment.body.length > 2048) { this.emit('engine_error'); return; }
      let runtime: unknown;
      try { runtime = JSON.parse(attachment.body.toString('utf8')); } catch { this.emit('engine_error'); return; }
      if (!validAndroidRuntime(runtime)) { this.emit('engine_error'); return; }
      this.emit('target_runtime', {testId: this.testId(test), retry: result.retry, runtime});
    }
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
