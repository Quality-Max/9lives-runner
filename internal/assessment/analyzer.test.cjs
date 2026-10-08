const { test } = require('node:test');
const assert = require('node:assert/strict');
const { createAnalyzer } = require('./analyzer.cjs');
const fs = require('node:fs');
const path = require('node:path');
const ts = require('typescript');
const analyze = createAnalyzer(ts);

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

test('suite skip modifiers are not declarations and apply to their whole scope', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test('file level', async () => { expect(1).toBe(1); });
    test.skip(isWeekend(), 'Only meaningful on business days');
    test.describe('conditional suite', () => {
      test('before modifier', async () => { expect(1).toBe(1); });
      test.fixme(({browserName}) => browserName === 'webkit', 'Known webkit issue');
      test.describe('nested', () => {
        test.beforeEach(async () => { if (flag) test.skip(); });
        test('nested', async () => { expect(1).toBe(1); });
      });
    });
    test.describe('disabled suite', () => {
      test.beforeEach(() => { test.skip(); });
      test('hook disabled', async () => { expect(1).toBe(1); });
    });
    test.describe('literal conditions', () => {
      test.skip(false, 'never');
      test('literal false', async () => { expect(1).toBe(1); });
      test.describe('always', () => {
        test.fixme(true, 'always');
        test('literal true', async () => { expect(1).toBe(1); });
      });
    });
    function guard() { test.skip(other(), 'helper'); }`);
  // Modifiers add no inventory, including the one inside an unattributable helper.
  assert.deepEqual(facts.tests.map(t => t.line), [2, 5, 9, 14, 18, 21]);
  assert(facts.tests.every(t => t.limits.length === 0));
  assert.deepEqual(facts.tests.map(t => t.disabled), [false, false, false, true, false, true]);
  assert.deepEqual(facts.tests.map(t => t.conditionalSkips.map(s => s.line)), [[3], [3, 6], [3, 6, 8], [3], [3], [3]]);
});

test('skip and fixme declarations stay disabled tests', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test.skip('literal title', async () => { expect(1).toBe(1); });
    test.fixme(\`template \${name}\`, helper);
    test.skip(computedTitle, async () => { expect(1).toBe(1); });`);
  assert.deepEqual(facts.tests.map(t => t.disabled), [true, true, true]);
  assert(facts.tests.every(t => t.conditionalSkips.length === 0));
});

test('literal titles are reported only on request', () => {
  const source = `import {test,expect} from '@playwright/test';
    test('checkout creates an order', () => { expect(1).toBe(1); });
    test(\`no substitution\`, () => { expect(1).toBe(1); });
    test(computedTitle, () => { expect(1).toBe(1); });`;
  assert(analyze(source).tests.every(t => !('title' in t)));
  assert.deepEqual(analyze(source, { titles: true }).tests.map(t => t.title), ['checkout creates an order', 'no substitution', undefined]);
  const long = analyze(`import {test} from '@playwright/test'; test('${'🙂'.repeat(300)}', () => {});`, { titles: true });
  assert.equal([...long.tests[0].title].length, 200);
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
  // @9l-requirement checkout-order
  scenario('checkout', async ({page}) => {
    const text = 'expect(x).toBe(1)'; // expect(x).toBe(1)
    // @9l-outcome confirmation
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
  assert.equal(facts.tests[0].assertions.length, 0);
});

test('parameterized titles retain callback assertions without evaluating declarations', () => {
  const facts = analyze("import {test,expect} from '@playwright/test';\n" +
    "for (const name of ['one', 'two']) {\n" +
    "  test(`case ${name}`, async ({page}) => { await expect(page.getByRole('heading')).toBeVisible(); });\n" +
    "}\n" +
    "cases.forEach(name => test(name, () => { expect(1).toBe(1); }));\n" +
    "test(computedTitle, () => { expect(2).toBe(2); });");
  // One syntax declaration per loop/callback, never a guessed runtime expansion.
  assert.equal(facts.tests.length, 3);
  assert.deepEqual(facts.tests.map(t => t.assertions.length), [1, 1, 1]);
  assert.deepEqual(facts.tests.map(t => t.limits.map(l => l.code)), [['declaration-generation'], ['declaration-generation'], []]);
  assert.equal(facts.tests[0].assertions[0].unawaited, false);
});

test('specific limits preserve observable waits and asynchronous matchers', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test.only('mixed', async ({page}) => {
      test.skip(condition);
      if (condition) helper();
      if (other) api.check();
      const later = () => expect(1).toBe(1);
      expect(page.getByRole('heading')).toBeVisible();
      await page.waitForTimeout(100);
    });
    test('external callback', callback);`);
  assert.deepEqual(facts.tests[0].limits.map(l => l.code), ['runtime-skip', 'conditional-flow', 'unresolved-helper', 'nested-function']);
  assert(facts.tests[0].limits.every(l => l.line > 0 && l.column > 0));
  assert.equal(facts.tests[0].sleeps.length, 1);
  assert.equal(facts.tests[0].assertions[1].unawaited, true);
  assert.equal(facts.tests[0].exclusive, true);
  assert.deepEqual(facts.tests[1].limits.map(l => l.code), ['dynamic-callback']);
});

test('helpers and enclosing expect shadows remain unknown', () => {
  const facts = analyze(`import {test,expect as check} from '@playwright/test';
    test('helper only', async () => { await verifyOrder(); });
    test('object helper', async () => { await assertions.verifyOrder(); });
    test.describe('shadowed', () => {
      const check = customCheck;
      test('nested', () => { check(1).toBe(1); });
    });
    test('page without assertions', async ({page: browserPage}) => { await browserPage.goto('/'); });`);
  assert.deepEqual(facts.tests.map(t => t.limits.map(l => l.code)), [['unresolved-helper'], ['unresolved-helper'], ['shadowed-binding'], []]);
  assert(facts.tests.every(t => t.assertions.length === 0));
});

test('loop and catch bindings cannot masquerade as imported assertions', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    for (const expect of customChecks) { test('loop', () => { expect(1).toBe(1); }); }
    try { something(); } catch (expect) { test('catch', () => { expect(1).toBe(1); }); }`);
  assert.equal(facts.tests.length, 2);
  assert(facts.tests.every(t => t.assertions.length === 0 && t.limits.some(l => l.code === 'shadowed-binding')));
});

test('calls through a test binding redeclared in an enclosing scope are not declarations', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    function register(test) { test('parameter', () => { expect(1).toBe(1); }); }
    for (const test of factories) { test('loop', () => {}); }
    test.describe('suite', () => {
      const test = custom;
      test('local', () => {});
      test.describe('nested', () => { test('inner', () => {}); });
    });
    try { register(); } catch (test) { test('catch', () => {}); }
    test('real', async ({page}) => { await expect(page).toHaveTitle('x'); });
    test('callback parameter', async ({test}) => { expect(1).toBe(1); });`);
  assert.deepEqual(facts.tests.map(t => t.line), [10, 11]);
  assert.deepEqual(facts.tests[0].limits, []);
  assert.equal(facts.tests[0].assertions.length, 1);
  // A redeclaration inside the callback keeps the declaration but stays unknown.
  assert.deepEqual(facts.tests[1].limits.map(l => l.code), ['shadowed-binding']);
});
