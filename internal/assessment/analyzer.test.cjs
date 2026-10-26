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
    test.skip(computedTitle, async () => { expect(1).toBe(1); });
    test.skip(name, run);
    test.fixme(c.title, helpers.run);
    for (const c of cases) test.skip(c.title, makeBody(c));
    test.skip(isMobile, REASON);`);
  // A string then a function is a declaration at runtime; ambiguous calls stay declarations.
  assert.deepEqual(facts.tests.map(t => t.line), [2, 3, 4, 5, 6, 7, 8]);
  assert(facts.tests.every(t => t.disabled && t.conditionalSkips.length === 0));
});

test('only calls that cannot declare a test are suite modifiers', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test.skip(true, REASON);
    test.describe('a', () => { test.fixme(!process.env.API, REASON); test.skip(browser === 'webkit', REASON); test.skip(isMobile); test('a', async () => { expect(1).toBe(1); }); });
    test('b', async () => { expect(1).toBe(1); });`);
  assert.deepEqual(facts.tests.map(t => t.line), [3, 4]);
  assert(facts.tests.every(t => t.disabled));
  assert.deepEqual(facts.tests.map(t => t.conditionalSkips.map(s => s.column)), [[32, 70, 111], []]);
});

test('after-hook modifiers run after test bodies and do not disable the suite', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test.describe('each', () => { test.afterEach(() => { test.skip(); }); test('e', async () => { expect(1).toBe(1); }); });
    test.describe('all', () => { test.afterAll(async () => test.fixme()); test('a', async () => { expect(1).toBe(1); }); });`);
  assert(facts.tests.every(t => !t.disabled));
  assert.deepEqual(facts.tests.map(t => t.conditionalSkips.map(s => s.line)), [[2], [3]]);
  assert(facts.tests.every(t => t.conditionalSkips.every(s => s.afterExecution)));
});

test('a shared suite modifier counts once toward the evidence limit', () => {
  const guards = Array.from({ length: 5 }, (_, i) => `test.skip(cond${i}(), 'reason');`).join('\n');
  const tests = Array.from({ length: 200 }, (_, i) => `test('t${i}', async () => { ${'expect(1).toBe(1); '.repeat(6)}});`).join('\n');
  const facts = analyze(`import {test,expect} from '@playwright/test';\n${guards}\n${tests}`);
  assert(facts.tests.every(t => t.conditionalSkips.length === 5));
  const nested = Array.from({ length: 17 }, (_, i) => `test.skip(cond${i}(), 'reason');`).join('\n');
  assert.throws(() => analyze(`import {test,expect} from '@playwright/test';\n${nested}\ntest('t', () => { expect(1).toBe(1); });`), { code: 'limit' });
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
  // Every other pinned matcher is synchronous unless chained through resolves/rejects.
  const generic = new Set();
  function genericVisit(node) {
    const members = ts.isInterfaceDeclaration(node) && ['GenericAssertions', 'SnapshotAssertions'].includes(node.name.text) ? node.members : [];
    for (const method of members) if (ts.isMethodSignature(method)) generic.add(method.name.getText(file));
    ts.forEachChild(node, genericVisit);
  }
  genericVisit(file);
  assert.ok(generic.has('toBe') && generic.has('toMatchSnapshot'));
  for (const matcher of generic) {
    const facts = analyze(`import {test,expect} from '@playwright/test';
      test('sync', () => { expect(value).${matcher}(); expect(value).not.${matcher}(); });
      test('resolves', async () => { expect(promise).resolves.${matcher}(); expect(promise).rejects.not.${matcher}(); await expect(promise).resolves.${matcher}(); });`);
    assert.deepEqual(facts.tests.map(t => t.assertions.map(a => a.unawaited)), [[false, false], [true, true, false]], matcher);
    assert(facts.tests.every(t => t.limits.length === 0), matcher);
  }
});

test('matchers outside the pinned API have unknown timing unless awaited or returned', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test('custom', async () => { expect(page).toHaveNewMatcher(); });
    test('awaited', async () => { await expect(page).toHaveNewMatcher(); });
    test('returned', () => { return expect(page).not.toHaveNewMatcher(); });`);
  assert.deepEqual(facts.tests.map(t => t.limits.map(l => l.code)), [['unknown-matcher'], [], []]);
  assert.deepEqual(facts.tests.map(t => t.assertions.length), [1, 1, 1]);
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

test('javascript specs are parsed as Playwright loads them: JSX allowed, TypeScript-only syntax rejected', () => {
  const spec = body => `import {test,expect} from '@playwright/test';\n${body}\ntest('x', async ({page}) => { await expect(page.getByRole('status')).toBeVisible(); });`;
  const typed = ["const n: string = 'x';", "interface A { x: number }", "const a = b as string;", "function f(a?) {}", "function g<T>(a) { return a; }", "const c = d!.e;", "import type { A } from './a';", "enum E { A }"];
  for (const body of typed) {
    assert.equal(analyze(spec(body)).tests.length, 1, body);
    assert.throws(() => analyze(spec(body), { javascript: true }), { code: 'typescript-in-javascript' }, body);
  }
  for (const body of ["const n = 'x';", "const El = () => <div>hi</div>;", "@dec class A {}"]) {
    const facts = analyze(spec(body), { javascript: true });
    assert.equal(facts.tests.length, 1, body);
    assert.equal(facts.tests[0].assertions.length, 1, body);
  }
  assert.throws(() => analyze("test(", { javascript: true }), { code: 'syntax' });
  assert.throws(() => analyze(spec("```ts"), { javascript: true }), { code: 'syntax' });
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

test('suite modifiers whose condition reads process.env are marked', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test.skip(!!process.env.CI, 'flaky in CI');
    test.describe('a', () => {
      test.beforeEach(() => { if (process.env['STAGE'] === 'prod') test.skip(); });
      test.fixme(({browserName}) => browserName === 'webkit', 'webkit');
      test.skip(true, process.env.REASON);
      test('a', async () => { expect(1).toBe(1); });
    });`);
  const [test] = facts.tests;
  assert.equal(test.disabled, true);
  assert.deepEqual(test.conditionalSkips.map(s => [s.line, s.environment, s.afterExecution]), [[2, true, false], [4, true, false], [5, false, false]]);
});

test('absence assertions are marked, and negated absence matchers are presence checks', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test('absence', async ({page}) => {
      await expect(page.getByRole('alert')).toBeHidden();
      await expect(page.getByRole('row')).toHaveCount(0);
      await expect(page).not.toHaveURL('/error');
      expect(count).toBe(0);
      expect(errors).toHaveLength(0);
      expect(sent).toBe(false);
      expect(error).toBeNull();
      await expect(page.getByText('x')).toBeVisible({ visible: false });
      await expect(page.getByText('x')).toBeAttached({ attached: false });
      await expect(promise).resolves.toBeFalsy();
    });
    test('presence', async ({page}) => {
      await expect(page.getByRole('alert')).toBeVisible();
      await expect(page.getByRole('alert')).not.toBeHidden();
      await expect(page.getByRole('row')).toHaveCount(3);
      await expect(page.getByRole('row')).not.toHaveCount(0);
      expect(count).toBe(1);
      expect(sent).toBe(true);
      await expect(page.getByText('x')).toBeVisible({ visible: true });
    });`);
  assert.deepEqual(facts.tests.map(t => t.assertions.map(a => a.absence)), [Array(10).fill(true), Array(7).fill(false)]);
});

const summary = t => ({ assertions: t.assertions.map(a => [a.line, a.unawaited, a.absence, a.afterWait]), sleeps: t.sleeps.map(s => s.line), limits: t.limits.map(l => l.code) });
// Facts inside an inlined helper carry the test's call site; direct facts carry none.
const sites = t => [...t.assertions, ...t.sleeps].map(f => f.site ? `${f.line}<${f.site.line}` : String(f.line));

test('same-file helpers are inlined at their call site in execution order', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    async function expectSaved(page) {
      await page.getByRole('button').click();
      await expect(page.getByText('Saved')).toBeVisible();
    }
    const expectGone = async (locator) => { await page2.waitForTimeout(1); await expect(locator).toBeHidden(); };
    const count = (rows) => expect(rows).toHaveCount(0);
    test('saved', async ({page}) => {
      await page.waitForTimeout(1);
      await expectSaved(page);
      await expectGone(page.getByRole('alert'));
      await count(page.getByRole('row'));
    });
    test('wait then helper absence', async ({page}) => {
      await page.waitForTimeout(1);
      await count(page.getByRole('row'));
    });`);
  // Facts keep the helper's own line and carry the test's call site; a fixture
  // argument makes the parameter a fixture root.
  assert.deepEqual(summary(facts.tests[0]), {
    assertions: [[4, false, false, true], [6, false, true, true], [7, false, true, false]],
    sleeps: [9, 6],
    limits: ['unresolved-helper'], // page2 inside expectGone is not a fixture
  });
  assert.deepEqual(sites(facts.tests[0]), ['4<10', '6<11', '7<12', '9', '6<11']);
  assert.equal(facts.tests[0].limits[0].line, 6);
  assert.deepEqual(summary(facts.tests[1]), { assertions: [[7, false, true, true]], sleeps: [15], limits: [] });
  assert.deepEqual(sites(facts.tests[1]), ['7<16', '15']);
});

test('nested helper facts carry the site of the call in the test', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    async function settle(page) { await page.waitForTimeout(50); }
    async function poll(page) { await settle(page); await expect(page.getByRole('status')).toHaveText('done'); }
    test('a', async ({page}) => { await poll(page); });
    test('b', async ({page}) => {
      await poll(page);
    });`);
  assert.deepEqual(facts.tests.map(sites), [['3<4', '2<4'], ['3<6', '2<6']]);
});

test('a helper wait followed by its own absence check stays ordered', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    async function closesToast(page) {
      await page.waitForTimeout(2000);
      await expect(page.getByRole('alert')).toBeHidden();
    }
    test('t', async ({page}) => { await closesToast(page); });`);
  assert.deepEqual(summary(facts.tests[0]), { assertions: [[4, false, true, true]], sleeps: [3], limits: [] });
  assert.deepEqual(sites(facts.tests[0]), ['4<6', '3<6']);
});

test('an awaited then/catch/finally chain consumes the matcher', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    const showMonth = async (root, arrow) => {
      await expect(root).not.toContainText('May').catch(() => arrow.click());
    };
    test('chains', async ({page}) => {
      await showMonth(page.locator('.cal'), page.locator('.next'));
      await expect(page).toHaveURL('/a').then(() => {}).finally(() => {});
      return expect(page).toHaveURL('/b').catch(() => {});
    });
    test('dropped', async ({page}) => {
      expect(page).toHaveURL('/c').catch(() => {});
      showMonth(page.locator('.cal'), page.locator('.next'));
    });`);
  assert.deepEqual(facts.tests.map(t => t.assertions.map(a => [a.line, a.unawaited, a.caught])),
    [[[3, false, true], [7, false, false], [8, false, true]], [[11, true, true], [3, true, true]]]);
  // The callbacks hold no assertion, so they are not a limit.
  assert.deepEqual(facts.tests.map(t => t.limits.map(l => l.code)), [[], []]);
});

test('a matcher whose failure is caught is a probe, and only a rejecting chain stays an assertion', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test('handled', async ({page}) => {
      await expect(page.getByRole('heading')).not.toContainText('May').catch(() => page.getByRole('button').click());
      await expect(page.getByRole('dialog')).toBeVisible().then(() => {}, () => {});
      await (expect(page).toHaveURL('/a')).then(() => {}).catch(handleFailure);
      await Promise.allSettled([expect(page.getByRole('row')).toHaveCount(1)]);
      await Promise.all([expect(page.getByRole('row')).toHaveCount(2)]).catch(() => {});
      try { await expect(page.getByRole('banner')).toBeVisible(); expect(count).toBe(1); } catch {}
      try { await expect(page.getByRole('link')).toBeVisible(); } catch (error) { log(error); } finally { done(); }
    });
    test('rejecting', async ({page}) => {
      await expect(page).toHaveURL('/b').then(() => {}).finally(() => {});
      await expect(page).toHaveURL('/c').catch();
      await expect(page).toHaveURL('/d').then(() => {}, undefined);
      await expect(page).toHaveURL('/e').catch(error => { throw error; });
      await expect(page).toHaveURL('/f').catch(error => Promise.reject(error));
      await Promise.all([expect(page.getByRole('row')).toHaveCount(3)]);
      try { await expect(page.getByRole('nav')).toBeVisible(); } catch (error) { cleanup(); throw error; }
      try { await expect(page.getByRole('main')).toBeVisible(); } finally { done(); }
      try { return expect(page.getByRole('aside')).toBeVisible(); } catch {}
    });`);
  assert.deepEqual(facts.tests.map(t => t.assertions.map(a => [a.line, a.caught])), [
    [[3, true], [4, true], [5, true], [6, true], [7, true], [8, true], [8, true], [9, true]],
    [[12, false], [13, false], [14, false], [15, false], [16, false], [17, false], [18, false], [19, false], [20, false]],
  ]);
  // Every assertion is still recorded, with its matcher; the probe is no
  // assertion, but its location stays visible.
  assert.deepEqual(facts.tests[0].assertions.map(a => a.matcher), ['toContainText', 'toBeVisible', 'toHaveURL', 'toHaveCount', 'toHaveCount', 'toBeVisible', 'toBe', 'toBeVisible']);
});

test('a handler is classified after unwrapping parentheses and type wrappers', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test('wrapped', async ({page}) => {
      await expect(page).toHaveURL('/a').catch((error => { throw error; }));
      await expect(page).toHaveURL('/b').catch((undefined));
      await expect(page).toHaveURL('/c').catch(((error: Error) => Promise.reject(error)) as never);
      await expect(page).toHaveURL('/d').then(() => {}, (undefined)!);
      await expect(page).toHaveURL('/e').catch(void 0);
      await expect(page).toHaveURL('/f').catch((null satisfies null));
      await expect(page).toHaveURL('/g').catch((() => {}));
    });`);
  assert.deepEqual(facts.tests[0].assertions.map(a => [a.line, a.caught]), [[3, false], [4, false], [5, false], [6, false], [7, false], [8, false], [9, true]]);
});

test('promise handlers catch only asynchronous failures, and try/catch also catches synchronous throws', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    function checkSync(value) { expect(value).toBe(1); }
    async function checkAsync(value) { expect(value).toBe(1); }
    test('direct', async ({page}) => {
      await Promise.allSettled([expect(actual).toEqual(expected)]);
      await Promise.all([expect(actual).toBe(1)]).catch(() => {});
      await Promise.allSettled([expect(page).toHaveURL('/a')]);
      await expect.poll(() => count).toBe(3).catch(() => {});
      await expect(pending).resolves.toBe(4).catch(() => {});
      try { await Promise.allSettled([expect(actual).toBe(2)]); } catch {}
    });
    test('helpers', async ({page}) => {
      await Promise.allSettled([checkSync(a)]);
      await checkAsync(a).catch(() => {});
      try { checkSync(b); } catch {}
    });
    test('custom', async ({page}) => {
      await expect(page).toBeCustom().catch(() => {});
      try { await expect(page).toBeCustom(); } catch {}
    });`);
  assert.deepEqual(facts.tests.map(t => t.assertions.map(a => [a.line, a.caught])), [
    [[5, false], [6, false], [7, true], [8, true], [9, true], [10, true]],
    [[2, false], [3, true], [2, true]],
    [[18, false], [19, true]],
  ]);
  // A custom matcher's timing is unknown, so a handler that catches only one
  // of a thrown failure and a rejected promise is a limit, not a probe. A
  // promise method chained on a helper call stays an unresolved call.
  assert.deepEqual(facts.tests.map(t => t.limits.map(l => [l.code, l.line])),
    [[['conditional-flow', 10]], [['unresolved-helper', 14], ['conditional-flow', 15]], [['unknown-matcher', 18], ['conditional-flow', 19]]]);
});

test('a returned helper promise is caught only when awaited inside the try block', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    function check(page) { return expect(page).toHaveURL('/ok'); }
    async function outer(page) { return check(page); }
    // @9l-assertion-helper
    function reviewed(page) { return expect(page).toHaveURL('/ok'); }
    test('return', async ({page}) => { try { return check(page); } catch {} });
    test('return await', async ({page}) => { try { return await check(page); } catch {} });
    test('through an async helper', async ({page}) => { await outer(page).catch(() => {}); });
    test('marked return', async ({page}) => { try { return reviewed(page); } catch {} });
    test('marked return await', async ({page}) => { try { return await reviewed(page); } catch {} });
    async function guarded(page) { try { return expect(page).toHaveURL('/x'); } catch {} }
    async function guardedAwait(page) { try { return await expect(page).toHaveURL('/y'); } catch {} }
    test('return inside a helper try', async ({page}) => { await guarded(page); await guardedAwait(page); });
    async function each(rows) { rows.forEach(async row => { await expect(row).toBeVisible(); }); }
    test('nested in a caught helper', async ({page}) => { try { await each(rows); } catch {} });`);
  assert.deepEqual(facts.tests.map(t => t.assertions.map(a => [a.line, a.caught])),
    [[[2, false]], [[2, true]], [[2, true]], [[9, false]], [[10, true]], [[11, false], [12, true]], [[14, false]]]);
  // A non-async marked helper may throw or return a promise; only one of them
  // is caught by an unawaited return, so that call is a limit.
  assert.deepEqual(facts.tests.map(t => t.limits.map(l => l.code)),
    [['conditional-flow'], ['conditional-flow'], ['unresolved-helper'], ['conditional-flow', 'unknown-matcher'], ['conditional-flow'], ['conditional-flow'],
      ['conditional-flow', 'unresolved-helper', 'nested-function']]);
});

test('a soft matcher fails the test whether or not it is caught', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    async function softCheck(page) { await expect.soft(page).toHaveURL('/y'); }
    test('soft', async ({page}) => {
      await expect.soft(page.getByRole('alert')).toBeVisible().catch(() => {});
      try { await expect.soft(page).toHaveURL('/x'); } catch {}
      await softCheck(page).catch(() => {});
      await expect.configure({ soft: true })(page).toHaveURL('/z').catch(() => {});
      await expect.configure({ timeout: 5 })(page).toHaveURL('/w').catch(() => {});
    });`);
  assert.deepEqual(facts.tests[0].assertions.map(a => [a.line, a.caught]), [[4, false], [5, false], [2, false], [7, false], [8, true]]);
});

test('a caught matcher whose expect may or may not be soft stays an assertion with a limit', () => {
  // Options the analyzer cannot read may or may not set soft: crediting the
  // matcher silently would overclaim, and calling it a probe would remove
  // credit from a check that may still fail the test.
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test('spread', async ({page}) => {
      await expect.configure({ ...options })(page).toHaveURL('/a').catch(() => {});
    });
    test('variable', async ({page}) => {
      await expect.configure(options)(page).toHaveURL('/b').catch(() => {});
    });
    test('runtime value', async ({page}) => {
      await expect.configure({ soft: flag })(page).toHaveURL('/c').catch(() => {});
    });
    test('soft overridden by a later spread', async ({page}) => {
      await expect.configure({ soft: true, ...options })(page).toHaveURL('/d').catch(() => {});
    });
    test('not caught', async ({page}) => {
      await expect.configure({ ...options })(page).toHaveURL('/e');
    });
    test('soft set after the spread', async ({page}) => {
      await expect.configure({ ...options, soft: true })(page).toHaveURL('/f').catch(() => {});
    });
    test('soft cleared after the spread', async ({page}) => {
      await expect.configure({ ...options, soft: false })(page).toHaveURL('/g').catch(() => {});
    });
    test('a later configure decides', async ({page}) => {
      await expect.configure({ soft: true }).configure({ soft: false })(page).toHaveURL('/h').catch(() => {});
      await expect.configure({ soft: false }).configure({ timeout: 5 })(page).toHaveURL('/i').catch(() => {});
    });`);
  assert.deepEqual(facts.tests.map(t => t.assertions.map(a => a.caught)),
    [[false], [false], [false], [false], [false], [false], [true], [true, true]]);
  assert.deepEqual(facts.tests.map(t => t.limits.map(l => [l.code, l.line])),
    [[['possibly-soft', 3]], [['possibly-soft', 6]], [['possibly-soft', 9]], [['possibly-soft', 12]], [], [], [], []]);
});

test('a call on a configured expect is not a matcher', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test('configured', async ({page}) => {
      await expect.configure({ timeout: 5 }).configure({ soft: true })(page).toHaveURL('/a');
      await expect.configure({ timeout: 5 }).soft(page).toHaveURL('/b');
    });`);
  assert.deepEqual(facts.tests[0].assertions.map(a => [a.line, a.matcher]), [[3, 'toHaveURL'], [4, 'toHaveURL']]);
});

test('a caught probe is no completion signal after a fixed wait and passes caught into helpers', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    // @9l-assertion-helper
    import { expectSaved } from './checks';
    async function confirm(page) { await expect(page.getByText('Saved')).toBeVisible(); }
    test('probe then absence', async ({page}) => {
      await page.waitForTimeout(500);
      await expect(page.getByText('Saved')).toBeVisible().catch(() => {});
      await expect(page.getByRole('alert')).toBeHidden();
    });
    test('caught helpers', async ({page}) => {
      await confirm(page).catch(() => {});
      try { await expectSaved(page); } catch {}
      await confirm(page);
    });`);
  assert.deepEqual(facts.tests[0].assertions.map(a => [a.line, a.caught, a.afterWait, a.absence]), [[7, true, true, false], [8, false, true, true]]);
  assert.deepEqual(facts.tests[1].assertions.map(a => [a.line, a.site?.line, a.caught, a.matcher]),
    [[4, 11, true, 'toBeVisible'], [12, undefined, true, 'assertion-helper'], [4, 13, false, 'toBeVisible']]);
});

test('a loop is not conditional flow, and is a limit only when it repeats a fixed wait', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    async function settle(page) { await page.waitForTimeout(50); }
    test('for of', async () => { for (const c of cases) { expect(c.input * 2).toEqual(c.output); } });
    test('for and while', async ({page}) => {
      for (let i = 0; i < 3; i++) await expect(page.getByRole('row').nth(i)).toBeVisible();
      while (await page.getByRole('button').isVisible()) { await page.getByRole('button').click(); }
      do { expect(n).toBeGreaterThan(0); } while (false);
    });
    test('wait in a loop', async ({page}) => {
      for (const id of ids) { await page.waitForTimeout(100); await expect(page.getByRole('alert')).toBeHidden(); }
    });
    test('helper wait in a loop', async ({page}) => { for (const id of ids) { await settle(page); expect(id).toBeTruthy(); } });
    test('branch in a loop', async ({page}) => { for (const c of cases) { if (c.skip) continue; expect(c.ok).toBe(true); } });
    test('branch', async ({page}) => { if (mobile) await expect(page.getByRole('heading')).toBeVisible(); });`);
  assert.deepEqual(facts.tests.map(t => t.limits.map(l => [l.code, l.line])),
    [[], [], [['loop', 10]], [['loop', 12]], [['conditional-flow', 13]], [['conditional-flow', 14]]]);
  assert.deepEqual(facts.tests.map(t => t.assertions.length), [1, 2, 1, 1, 1, 1]);
});

test('a wait that a loop header evaluates once does not make the loop a limit', () => {
  // A for initializer and a for…of or for…in input run once; a for condition
  // and update, and a while or do condition, repeat with the body.
  const facts = analyze(`import {test,expect} from '@playwright/test';
    async function loadRows(page) { await page.waitForTimeout(50); return rows; }
    test('for of input', async ({page}) => { for (const row of await loadRows(page)) { expect(row.id).toBeDefined(); } });
    test('for in input', async ({page}) => { for (const key in await loadRows(page)) { expect(key).toBeTruthy(); } });
    test('for initializer', async ({page}) => { for (let rows = await loadRows(page), i = 0; i < rows.length; i++) { expect(rows[i]).toBeTruthy(); } });
    test('for condition', async ({page}) => { for (let i = 0; i < (await loadRows(page)).length; i++) { expect(i).toBeGreaterThan(-1); } });
    test('for update', async ({page}) => { for (let i = 0; i < 3; i++, await page.waitForTimeout(10)) { expect(i).toBeGreaterThan(-1); } });
    test('while condition', async ({page}) => { while (await loadRows(page)) { expect(1).toBe(1); } });
    test('do condition', async ({page}) => { do { expect(1).toBe(1); } while (await loadRows(page)); });
    test('once, then repeated', async ({page}) => { for (const row of await loadRows(page)) { await page.waitForTimeout(5); expect(row).toBeTruthy(); } });`);
  assert.deepEqual(facts.tests.map(t => t.limits.map(l => [l.code, l.line])),
    [[], [], [], [['loop', 6]], [['loop', 7]], [['loop', 8]], [['loop', 9]], [['loop', 10]]]);
  // Scanning order is unchanged: every wait is still recorded, and a header
  // wait is read before the body (a do condition after it).
  assert.deepEqual(facts.tests.map(t => [t.sleeps.length, t.assertions.map(a => a.afterWait)]),
    [[1, [true]], [1, [true]], [1, [true]], [1, [true]], [1, [true]], [1, [true]], [1, [false]], [2, [true]]]);
});

test('a non-callable rejection handler is ignored, so the matcher still rejects', () => {
  // A promise ignores a handler that is not a function, so the rejection
  // passes on down the chain. Identifiers and call results may be functions
  // and are still taken to swallow it.
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test('non-callable', async ({page}) => {
      await expect(page).toHaveURL('/a').catch(false);
      await expect(page).toHaveURL('/b').catch(0);
      await expect(page).toHaveURL('/c').catch('x');
      await expect(page).toHaveURL('/d').catch([]);
      await expect(page).toHaveURL('/e').catch({});
      await expect(page).toHaveURL('/f').then(() => {}, false);
      await expect(page).toHaveURL('/g').catch(\`x\${y}\`);
      await expect(page).toHaveURL('/h').catch(\`x\`);
      await expect(page).toHaveURL('/i').catch((true as any));
      await expect(page).toHaveURL('/j').catch(-1);
      await expect(page).toHaveURL('/k').catch(/x/);
      await expect(page).toHaveURL('/l').catch(10n);
      await expect(page).toHaveURL('/m').then(() => {}, ({ handle() {} }));
      await expect(page).toHaveURL('/n').catch(!flag);
      await expect(page).toHaveURL('/o').catch(typeof handler);
    });
    test('possibly callable or caught later', async ({page}) => {
      await expect(page).toHaveURL('/p').catch(false).catch(() => {});
      await expect(page).toHaveURL('/q').catch(handleFailure);
      await expect(page).toHaveURL('/r').catch(makeHandler());
      await expect(page).toHaveURL('/s').then(false, () => {});
      try { await expect(page).toHaveURL('/t').catch(false); } catch {}
    });`);
  assert.deepEqual(facts.tests.map(t => t.assertions.map(a => a.caught)),
    [Array(15).fill(false), [true, true, true, true, true]]);
});

test('a poll is recognized from its structure, however it is configured or spaced', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test('polls', async () => {
      await expect.configure({ timeout: 5 }).poll(() => count).toBe(3).catch(() => {});
      await expect.poll (() => count).toBe(3).catch(() => {});
      await expect
        .poll(() => count).not.toBe(3).catch(() => {});
      await expect.configure({ timeout: 5 }).configure({ intervals: [1] }).poll(() => count).toBe(3).catch(() => {});
      expect.configure({ timeout: 5 }).poll(() => count).toBe(3);
      try { expect.poll (() => count).toBe(3); } catch {}
    });
    test('not polls', async () => {
      await expect.configure({ timeout: 5 })(count).toBe(3).catch(() => {});
      await expect(poll(() => count)).toBe(3).catch(() => {});
    });`);
  assert.deepEqual(facts.tests.map(t => t.assertions.map(a => [a.line, a.caught, a.unawaited])), [
    [[3, true, false], [4, true, false], [5, true, false], [7, true, false], [8, false, true], [9, false, true]],
    [[12, false, false], [13, false, false]],
  ]);
});

test('a nested function is a limit only when it holds an assertion or wait', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    const settle = async page => { await page.waitForTimeout(100); };
    test('projections', async ({page}) => {
      const tokens = ['a', 'b'].map((f) => f.length);
      const sorted = (a, b) => a - b;
      expect(tokens.sort(sorted)).toEqual([1, 1]);
    });
    test('outermost location', async ({page}) => {
      const run = async () => {
        const inner = () => expect(1).toBe(1);
        await inner();
      };
    });
    test('inlined helper', async ({page}) => {
      page.on('load', () => settle(page));
    });
    test('browser side', async ({page}) => {
      const attributes = await page.evaluate((names) => {
        const found = [];
        for (const el of Array.from(document.querySelectorAll('*'))) if (el.title) found.push(names, helper(el));
        return found;
      }, ['title']);
      await page.locator('a').evaluateAll(links => links.map(link => link.href));
      await page.addInitScript(() => { if (window.flag) expect(1).toBe(2); });
      expect(attributes).toEqual([]);
    });`);
  const limits = facts.tests.map(t => t.limits.map(l => [l.code, l.line]));
  assert.deepEqual(limits, [
    // Calls on unknown values stay limits; the projections themselves do not.
    [['unresolved-helper', 4]],
    // Reported once, at the outermost function holding the assertion.
    [['unresolved-helper', 11], ['nested-function', 9]],
    // An inlined helper's wait is held by the listener.
    [['nested-function', 15]],
    // Page functions are not scanned.
    [],
  ]);
  assert.deepEqual(facts.tests.map(t => t.unsupported), [true, true, true, false]);
  assert.deepEqual(facts.tests.map(t => [t.assertions.length, t.sleeps.length]), [[1, 0], [1, 0], [0, 1], [1, 0]]);
});

test('a nested function returning the matcher is not unawaited unless forEach discards it', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test('rendered map', async ({page}) => {
      const rendered = {
        heading: () => expect(page.getByRole('heading')).toBeVisible(),
        rows: () => { return expect(page.getByRole('row')).toHaveCount(3); },
        status: async () => { await expect(page.getByRole('status')).toBeVisible(); },
      };
      for (const check of Object.values(rendered)) await check();
      await Promise.all([1, 2].map(n => expect(page.getByText(String(n))).toBeVisible()));
      [page.locator('a')].forEach(l => expect(l).toBeVisible());
      [page.locator('b')].forEach(l => { return expect(l).toBeVisible(); });
      [page.locator('c')].forEach(l => { expect(l).toBeVisible(); });
    });`);
  assert.deepEqual(facts.tests[0].assertions.map(a => [a.line, a.unawaited]),
    [[4, false], [5, false], [6, false], [9, false], [10, true], [11, true], [12, true]]);
  assert(facts.tests[0].limits.some(l => l.code === 'nested-function'));
});

test('parenthesized async matchers are recognized as consumed', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test('concise return', async ({page}) => { const check = () => (expect(page).toHaveURL('/')); check(); });
    test('explicit return', async ({page}) => { const check = () => { return (expect(page).toHaveURL('/')); }; check(); });
    test('await', async () => { await (expect(page).toHaveURL('/')); });
    test('parenthesized chain', async () => { await (expect(page).toHaveURL('/')).then(() => {}); });`);
  assert.deepEqual(facts.tests.map(t => t.assertions.map(a => a.unawaited)), [[false], [false], [false], [false]]);
});

test('an empty array equality after a fixed wait is an absence check', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test('no mutations', async ({page}) => {
      await page.waitForTimeout(300);
      expect(mutations).toEqual([]);
      expect(calls).toStrictEqual([]);
      expect(items).toEqual([1]);
      expect(items).not.toEqual([]);
    });`);
  assert.deepEqual(facts.tests[0].assertions.map(a => [a.absence, a.afterWait]), [[true, true], [true, false], [false, false], [false, false]]);
});

test('unconsumed helper promises make their assertions unawaited', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    async function asyncCheck(page) { expect(1).toBe(1); await expect(page).toHaveURL('/'); }
    function returnsCheck(page) { return expect(page).toHaveURL('/'); }
    function syncCheck() { expect(1).toBe(1); }
    test('floating', async ({page}) => { asyncCheck(page); returnsCheck(page); syncCheck(); });
    test('consumed', async ({page}) => { await asyncCheck(page); await returnsCheck(page); syncCheck(); });
    test('returned', ({page}) => { return returnsCheck(page); });`);
  assert.deepEqual(facts.tests.map(t => t.assertions.map(a => a.unawaited)), [[true, true, true, false], [false, false, false, false], [false]]);
  assert(facts.tests.every(t => t.limits.length === 0));
});

test('unresolvable helpers stay unknown and never add assertions', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    import { importedCheck } from './helpers';
    let reassigned = () => expect(1).toBe(1);
    function recursive(page) { recursive(page); }
    const h1 = () => h2(); const h2 = () => h3(); const h3 = () => h4(); const h4 = () => h5(); const h5 = () => expect(1).toBe(1);
    test('imported', async () => { await importedCheck(); });
    test('let', async () => { reassigned(); });
    test('parameter', async ({ login }) => { await login(); });
    test('recursive', async ({page}) => { recursive(page); });
    test('too deep', async () => { h1(); });
    test('shadowed helper', async () => { const expectSaved = other; expectSaved(); });
    function expectSaved() { expect(1).toBe(1); }`);
  assert.deepEqual(facts.tests.map(t => [t.assertions.length, t.limits.map(l => l.code)]), [
    [0, ['unresolved-helper']], [0, ['unresolved-helper']], [0, ['unresolved-helper']],
    [0, ['unresolved-helper']], [0, ['unresolved-helper']], [0, ['unresolved-helper']]]);
});

test('helper limits, shadowing and outcome annotations carry through', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    function branchy(page) { if (flag) { expect(1).toBe(1); } }
    function shadows(expect) { expect(1).toBe(1); }
    function mapped(page) {
      // @9l-outcome order-created
      expect(1).toBe(1);
    }
    test('branch', async ({page}) => { branchy(page); });
    test('shadow', async ({page}) => { shadows(page); });
    test('mapped', async ({page}) => { mapped(page); });`);
  assert.deepEqual(facts.tests.map(t => t.limits.map(l => [l.code, l.line])), [[['conditional-flow', 2]], [['shadowed-binding', 3]], []]);
  assert.deepEqual(facts.tests.map(t => t.assertions.length), [1, 0, 1]);
  assert.deepEqual(facts.tests[2].assertions[0].outcomes, ['order-created']);
});

test('marked assertion helpers count as one assertion without inference', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    // @9l-assertion-helper
    import { expectOrderSaved, expectToast } from './checks';
    import { submit } from './actions';
    // @9l-assertion-helper
    async function expectRow(page) { await page.waitForTimeout(1); await opaque(page); }
    /** @9l-assertion-helper */
    const expectTitle = (page) => opaque(page);
    test('marked', async ({page}) => {
      await page.waitForTimeout(1);
      // @9l-outcome order-saved
      await expectOrderSaved(page);
      expectRow(page);
      expectTitle(page);
      expectToast(page);
      await submit(page);
    });`);
  const [t] = facts.tests;
  assert.deepEqual(t.assertions.map(a => [a.line, a.unawaited, a.absence, a.afterWait, a.outcomes]), [
    [12, false, false, true, ['order-saved']], [13, true, false, false, []], [14, false, false, false, []], [15, false, false, false, []]]);
  // Bodies are not inferred: no inner wait or helper; the unmarked import and an
  // unconsumed imported helper of unknown timing stay limits.
  assert.deepEqual(t.sleeps.map(s => s.line), [10]);
  // A non-async marked helper and an imported one have unknown timing when unconsumed.
  assert.deepEqual(t.limits.map(l => [l.code, l.line]), [['unknown-matcher', 14], ['unresolved-helper', 16]]);
});

test('review regressions: helper resolution never invents or hides assertions', () => {
  const run = source => analyze(`import {test,expect} from '@playwright/test';\n${source}`).tests.map(t => ({
    assertions: t.assertions.map(a => [a.line, a.unawaited]), limits: t.limits.map(l => l.code) }));
  // Calling a generator does not run its body.
  assert.deepEqual(run(`function* gen() { expect(1).toBe(1); }
    async function* agen() { expect(1).toBe(1); }
    test('g', async () => { gen(); await agen(); });`), [{ assertions: [], limits: ['unresolved-helper'] }]);
  // Any second binding of the name, in any scope, leaves the call unresolved.
  for (const shadow of ['{ var check = Function.prototype; }', 'switch (x) { case 1: const check = other; }']) {
    assert.deepEqual(run(`function check() { expect(1).toBe(1); }
      test('t', async () => { ${shadow} check(); });`)[0].assertions, [], shadow);
  }
  assert.deepEqual(run(`function check() { expect(1).toBe(1); }
    namespace N { function check() {} test('n', async () => { check(); }); }`)[0].assertions, []);
  // A helper declared inside the test is scanned once, as a nested function.
  assert.deepEqual(run(`test('inner', async () => { const check = () => { expect(1).toBe(1); }; check(); });`),
    [{ assertions: [[2, false]], limits: ['nested-function', 'unresolved-helper'] }]);
  // forEach discards a concise arrow's promise; a nested arrow elsewhere returns it to an unknown caller.
  assert.deepEqual(run(`test('t', async ({page}) => { [page.locator('a')].forEach(l => expect(l).toBeVisible()); });`)[0].assertions, [[2, true]]);
  assert.deepEqual(run(`test('t', async ({page}) => { const check = () => expect(page).toHaveURL('/'); await check(); });`)[0].assertions, [[2, false]]);
  // A non-async marked helper's timing is unknown when unconsumed.
  assert.deepEqual(run(`// @9l-assertion-helper
    function check(page) { return expect(page).toHaveURL('/x'); }
    test('t', async ({page}) => { check(page); });`)[0].limits, ['unknown-matcher']);
  // A marker mentioned in prose marks nothing.
  assert.deepEqual(run(`/* Helpers here are tagged @9l-assertion-helper where reviewed. */
    import { login } from './auth';
    test('t', async ({page}) => { await login(page); });`), [{ assertions: [], limits: ['unresolved-helper'] }]);
});

test('inlining stays within the fact and work budgets instead of failing the file', () => {
  const checks = Array.from({ length: 10 }, () => 'await expect(page.getByRole("a")).toBeVisible();').join(' ');
  const tests = Array.from({ length: 30 }, (_, i) => `test('t${i}', async ({page}) => { ${'await check(page); '.repeat(7)}});`).join('\n');
  const facts = analyze(`import {test,expect} from '@playwright/test';\nasync function check(page) { ${checks} }\n${tests}`);
  const total = facts.tests.reduce((n, t) => n + t.assertions.length, 0);
  assert(total <= 2048 && total >= 1900, String(total));
  assert(facts.tests.at(-1).limits.some(l => l.code === 'unresolved-helper'));
  // A large helper called many times is abandoned quickly rather than exhausting time or memory.
  const big = Array.from({ length: 5000 }, () => 'expect(1).toBe(1);').join(' ');
  const started = Date.now();
  const many = analyze(`import {test,expect} from '@playwright/test';\nfunction check() { ${big} }\n${Array.from({ length: 20 }, (_, i) => `test('t${i}', async () => { ${'check(); '.repeat(64)}});`).join('\n')}`);
  assert(Date.now() - started < 5000, `took ${Date.now() - started} ms`);
  assert(many.tests.reduce((n, t) => n + t.assertions.length, 0) <= 2048);
});

test('re-review regressions: consumed concise helpers and the direct fact reservation', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    async function b(page) { await expect(page).toHaveURL('/x'); }
    const a = (page) => b(page);
    test('floating', async ({ page }) => { a(page); });
    test('awaited', async ({ page }) => { await a(page); });`);
  assert.deepEqual(facts.tests.map(t => t.assertions.map(x => x.unawaited)), [[true], [false]]);
  // Inlined facts leave room for direct facts declared later in the file.
  const checks = Array.from({ length: 10 }, () => 'await expect(page.getByRole("a")).toBeVisible();').join(' ');
  const inlined = Array.from({ length: 20 }, (_, i) => `test('t${i}', async ({page}) => { ${'await check(page); '.repeat(7)}});`).join('\n');
  const direct = `test('direct', async ({page}) => { ${'expect(1).toBe(1); '.repeat(700)}});`;
  const result = analyze(`import {test,expect} from '@playwright/test';\nasync function check(page) { ${checks} }\n${inlined}\n${direct}`);
  assert.equal(result.tests.at(-1).assertions.length, 700);
  assert(result.tests.reduce((n, t) => n + t.assertions.length, 0) <= 2048);
});

test('a test imported from an unrecognised module is named, not silently empty', () => {
  const foreign = analyze(`import {test, expect} from 'e2e';
    import {test as fixtureTest} from './fixtures';
    test('login', async () => { expect(1).toBe(1); });`);
  assert.deepEqual(foreign.tests, []);
  assert.deepEqual(foreign.unrecognizedTestImports, ['./fixtures', 'e2e']);
  const recognised = analyze(`import {test, expect} from '@playwright/test';
    import {test as other} from 'e2e';
    test('login', async () => { expect(1).toBe(1); });`);
  assert.equal(recognised.tests.length, 1);
  assert.equal(recognised.unrecognizedTestImports, undefined);
  const odd = analyze(`import {test} from 'some module with spaces';`);
  assert.deepEqual(odd.unrecognizedTestImports, ['(unnamed module)']);
});

test('a promise is followed into Promise.all and through the callback that carries it', () => {
  const facts = analyze(`import {test, expect} from '@playwright/test';
    test('all awaited', async ({page}) => {
      await Promise.all([expect(page.locator('a')).toBeVisible(), (expect(page.locator('b')).toBeVisible())]);
    });
    test('allSettled returned', ({page}) => {
      return Promise.allSettled([expect(page.locator('a')).toBeVisible()]).then(() => {});
    });
    test('all dropped', async ({page}) => {
      Promise.all([expect(page.locator('a')).toBeVisible()]);
    });
    test('parenthesized forEach callback', async ({page}) => {
      [page.locator('a')].forEach((l => expect(l).toBeVisible()));
    });
    test('map result dropped', async ({page}) => {
      [page.locator('a')].map(l => expect(l).toBeVisible());
    });
    test('map result awaited', async ({page}) => {
      await Promise.all([page.locator('a')].map(l => expect(l).toBeVisible()));
    });`);
  const unawaited = facts.tests.map(t => t.assertions.map(a => a.unawaited));
  assert.deepEqual(unawaited, [[false, false], [false], [true], [true], [true], [false]]);
  // Promise combinators are not unknown helpers; array callbacks stay limits.
  assert.deepEqual(facts.tests.map(t => t.limits.map(l => l.code)), [[], [], [], ['unresolved-helper', 'nested-function'], ['unresolved-helper', 'nested-function'], ['unresolved-helper', 'nested-function']]);
});

const kinds = t => t.hazards.map(h => (h.site ? `${h.kind}@${h.line}<${h.site.line}` : `${h.kind}@${h.line}`));

test('debugging leftovers and structural or positional selectors are hazards', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    const pause = (page) => page.pause();
    test('t', async ({page: tab}) => {
      debugger;
      await pause(tab);
      await tab.locator('xpath=//form').click();
      await tab.locator('(//a)[2]').click();
      await tab.$$('..');
      await tab.locator('css=nav >> li:nth-of-type(2)').click();
      await tab.locator('li >> nth=0').click();
      await tab.locator('li:nth-match(:text("A"), 2)').click();
      await tab.locator('li').last().click();
      await tab.locator('li').nth(-1).click();
      await tab.click('td:first-child');
      await tab.fill('//input', 'x');
      await tab.dragAndDrop('#a', '//div[@id="b"]');
      await tab.getByRole('alert', {exact: true}).click();
    });`);
  assert.deepEqual(kinds(facts.tests[0]), ['debugger-statement@4', 'page-pause@2<5', 'xpath@6', 'xpath@7', 'xpath@8', 'positional-css@9',
    'position@10', 'position@11', 'position@12', 'position@13', 'positional-css@14', 'xpath@15', 'xpath@16']);
  assert.equal(facts.version, 11);
});

test('values, keys, role locators and non-page pauses are not hazards', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test('t', async ({page}) => {
      await page.getByLabel('URL').fill('//example.com');
      await page.getByLabel('Path').type('../up', {delay: 1});
      await page.getByRole('textbox', {name: 'Q'}).first;
      await page.keyboard.press('..');
      await page.getByRole('button', {name: 'Save'}).click();
      await page.getByRole('button').click();
      await expect(page.getByRole('status')).toBeVisible();
      await expect(page.getByRole('alert', {exact: true})).toBeVisible();
      await page.getByRole('button', { name }).click();
      await page.getByRole('button', options).click();
      await page.getByRole('button', {...options}).click();
      await page.locator('li').nth(index).click();
      await page.locator(\`//\${dynamic}\`).click();
      await page.locator('.card:hover').click();
      const player = {pause() {}};
      player.pause();
      await page.evaluate(() => { debugger; document.querySelector('li:nth-child(2)'); });
      // page.locator('//commented')
      const text = "await page.pause(); debugger; locator('//x')";
    });`);
  assert.deepEqual(kinds(facts.tests[0]), []);
});

test('hazards in nested functions are reported without making the function a limit', () => {
  const facts = analyze(`import {test,expect} from '@playwright/test';
    test('t', async ({page}) => {
      const open = async () => { await page.locator('//nav').click(); };
      await open();
      await expect(page).toHaveURL('/');
    });`);
  assert.deepEqual(kinds(facts.tests[0]), ['xpath@3']);
  assert.deepEqual(facts.tests[0].limits.map(l => l.code), ['unresolved-helper']);
});

test('an abandoned helper expansion drops its hazards with its other facts', () => {
  // The helper's matchers exceed the inline fact budget, so its expansion is abandoned.
  const big = 'debugger; ' + Array.from({ length: 2100 }, () => 'expect(1).toBe(1);').join(' ');
  const facts = analyze(`import {test,expect} from '@playwright/test';\nfunction check() { ${big} }\ntest('t', async () => { check(); expect(1).toBe(1); });`);
  assert.equal(facts.tests[0].hazards.length, 0);
  assert.deepEqual(facts.tests[0].limits.map(l => l.code), ['unresolved-helper']);
  assert.equal(facts.hazardsTruncated, undefined);
});

test('hazards have their own budget and never cut assertion analysis short', () => {
  // The same helper hazard is reported once per test, however often it is called.
  const once = analyze(`import {test,expect} from '@playwright/test';
    async function open(page) { await page.locator('//nav').click(); }
    test('t', async ({page}) => { await open(page); await open(page); await expect(page).toHaveURL('/'); });`);
  assert.deepEqual(kinds(once.tests[0]), ['xpath@2<3']);
  const checks = Array.from({ length: 10 }, () => 'await expect(page.locator("//a")).toBeVisible();').join(' ');
  const tests = Array.from({ length: 60 }, (_, i) => `test('t${i}', async ({page}) => { await check(page); });`).join('\n');
  const facts = analyze(`import {test,expect} from '@playwright/test';\nasync function check(page) { ${checks} }\n${tests}`);
  assert.equal(facts.tests.reduce((n, t) => n + t.assertions.length, 0), 600);
  assert(facts.tests.every(t => t.limits.length === 0));
  assert.equal(facts.tests.reduce((n, t) => n + t.hazards.length, 0), 512);
  assert.equal(facts.hazardsTruncated, true);
});
