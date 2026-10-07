const { test } = require('node:test');
const assert = require('node:assert/strict');
const { analyze } = require('./analyzer.cjs');
const fs = require('node:fs');
const path = require('node:path');
const ts = require('typescript');

test('disabled declarations and enclosing suites retain their state', () => {
  for (const modifier of ['skip', 'fixme']) {
    const facts = analyze(`import {test as scenario,expect} from '@playwright/test';
      scenario.${modifier}('direct', async () => { expect(1).toBe(1); });
      scenario.describe.${modifier}('disabled suite', () => {
        scenario.describe('nested suite', () => {
          scenario('inherited', async () => { expect(1).toBe(1); });
        });
      });
      scenario.describe('enabled suite', () => {
        scenario('enabled', async () => { expect(1).toBe(1); });
      });`);
    assert.deepEqual(facts.tests.map(t => t.disabled), [true, true, false], modifier);
  }
});

test('conditional body skips remain unsupported rather than assumed disabled', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test('conditional', async () => {
      test.skip(condition, 'conditional skip');
      expect(1).toBe(1);
    });`);
  assert.equal(facts.tests[0].disabled, false);
  assert.equal(facts.tests[0].unsupported, true);
});

test('all pinned Playwright async matchers require await or return', () => {
  // Derive cases from the external API contract, not the analyzer's allowlist.
  const types = fs.readFileSync(path.join(path.dirname(require.resolve('playwright/package.json')), 'types/test.d.ts'), 'utf8');
  const file = ts.createSourceFile('test.d.ts', types, ts.ScriptTarget.Latest, true);
  const matchers = new Set();
  function visit(node) {
    const members = ts.isInterfaceDeclaration(node) && ['GenericAssertions', 'LocatorAssertions', 'PageAssertions', 'APIResponseAssertions'].includes(node.name.text) ? node.members
      : ts.isTypeAliasDeclaration(node) && node.name.text === 'FunctionAssertions' && ts.isTypeLiteralNode(node.type) ? node.type.members : [];
    for (const method of members) {
      if (ts.isMethodSignature(method) && method.type?.getText(file) === 'Promise<void>') matchers.add(method.name.getText(file));
    }
    ts.forEachChild(node, visit);
  }
  visit(file);
  assert.ok(matchers.has('toHaveRole') && matchers.has('toBeOK') && matchers.has('toPass'));
  for (const matcher of matchers) {
    const facts = analyze(`import {test,expect as check} from '@playwright/test';
      test('unawaited', async () => { check(target).${matcher}(); });
      test('negated', async () => { check(target).not.${matcher}(); });
      test('awaited', async () => { await check(target).${matcher}(); });
      test('returned', () => { return check(target).${matcher}(); });`);
    assert.deepEqual(facts.tests.map(t => t.assertions[0].unawaited), [true, true, false, false], matcher);
  }
  const sync = analyze(`import {test,expect} from '@playwright/test';
    test('sync', () => { expect(1).toBe(1); expect('x').toMatchSnapshot(); });`);
  assert.deepEqual(sync.tests[0].assertions.map(a => a.unawaited), [false, false]);
});

test('syntax, aliases and annotations; comments and strings are not assertions', () => {
  const facts = analyze(`import { test as scenario, expect as check } from '@playwright/test';
  // @9lives-requirement checkout-order
  scenario('checkout', async ({page}) => {
    const text = 'expect(x).toBe(1)'; // expect(x).toBe(1)
    // @9lives-outcome confirmation
    await check(page.getByRole('status')).toHaveText('Complete');
  });`);
  assert.equal(facts.tests.length, 1);
  assert.deepEqual(facts.tests[0].requirements, ['checkout-order']);
  assert.equal(facts.tests[0].assertions.length, 1);
  assert.deepEqual(facts.tests[0].assertions[0].outcomes, ['confirmation']);
  assert.equal(facts.tests[0].assertions[0].unawaited, false);
});

test('async assertion handling and conditional helper analysis limits', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
  test.only('x', async ({page}) => {
    expect(page.locator('x')).toBeVisible();
    expect(1).toBe(1);
    await page.waitForTimeout(100);
    if (true) helper();
  });`);
  assert.equal(facts.tests[0].exclusive, true);
  assert.equal(facts.tests[0].unsupported, true);
  assert.equal(facts.tests[0].sleeps.length, 1);
  assert.deepEqual(facts.tests[0].assertions.map(a => a.unawaited), [true, false]);
});

test('dynamic callbacks remain unknown and invalid syntax is rejected', () => {
  assert.equal(analyze(`import {test} from '@playwright/test'; test('x', helper);`).tests[0].unsupported, true);
  assert.throws(() => analyze('test('));
});

test('source and imports are parsed without execution, shadowed aliases stay unsupported', () => {
  const facts = analyze(`import {test, expect as $check} from '@playwright/test';
  import './does-not-exist';
  throw new Error('must not execute this module');
  test('x', async ({ $check }) => { $check(1).toBe(1); });`);
  assert.equal(facts.tests.length, 1);
  assert.equal(facts.tests[0].unsupported, true);
});
