'use strict';

// Parse source only. Never import a spec, configuration, or application module.
class AnalysisError extends Error {
  constructor(code) { super(code); this.code = code; }
}

function createAnalyzer(ts) {
  if (ts.version !== '5.9.3' || typeof ts.createSourceFile !== 'function') throw new AnalysisError('parser-unavailable');
  const idPattern = /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,79}$/;
  // Playwright 1.61.1's locator, page, API response and function async matchers.
  // The regression suite checks this against the pinned public type declarations.
  const asyncMatchers = new Set([
    'toBeAttached', 'toBeChecked', 'toBeDisabled', 'toBeEditable', 'toBeEmpty',
    'toBeEnabled', 'toBeFocused', 'toBeHidden', 'toBeInViewport', 'toBeVisible',
    'toContainClass', 'toContainText', 'toHaveAccessibleDescription',
    'toHaveAccessibleErrorMessage', 'toHaveAccessibleName', 'toHaveAttribute',
    'toHaveClass', 'toHaveCount', 'toHaveCSS', 'toHaveId', 'toHaveJSProperty',
    'toHaveRole', 'toHaveScreenshot', 'toHaveText', 'toHaveValue', 'toHaveValues',
    'toHaveTitle', 'toHaveURL', 'toMatchAriaSnapshot', 'toBeOK', 'toPass',
  ]);
  // Its generic and snapshot matchers, which are synchronous unless chained
  // through resolves/rejects. Any other matcher, such as a custom one or one
  // from a newer release, has unknown timing.
  const syncMatchers = new Set([
    'toBe', 'toBeCloseTo', 'toBeDefined', 'toBeFalsy', 'toBeGreaterThan',
    'toBeGreaterThanOrEqual', 'toBeInstanceOf', 'toBeLessThan', 'toBeLessThanOrEqual',
    'toBeNaN', 'toBeNull', 'toBeTruthy', 'toBeUndefined', 'toContain', 'toContainEqual',
    'toEqual', 'toHaveLength', 'toHaveProperty', 'toMatch', 'toMatchObject',
    'toStrictEqual', 'toThrow', 'toThrowError', 'toMatchSnapshot',
  ]);

  function analyze(source, options = {}) {
    const file = ts.createSourceFile('input.tsx', source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
    if (file.parseDiagnostics.length) throw new AnalysisError('syntax');
    const tests = new Set(), expects = new Set();
    for (const statement of file.statements) {
      if (!ts.isImportDeclaration(statement) || !['@playwright/test', '@9l/playwright'].includes(statement.moduleSpecifier.text)) continue;
      const bindings = statement.importClause?.namedBindings;
      if (!bindings || !ts.isNamedImports(bindings)) continue;
      for (const binding of bindings.elements) {
        const imported = (binding.propertyName || binding.name).text;
        if (imported === 'test') tests.add(binding.name.text);
        if (imported === 'expect') expects.add(binding.name.text);
      }
    }
    const location = node => {
      const pos = file.getLineAndCharacterOfPosition(node.getStart(file));
      return { line: pos.line + 1, column: pos.character + 1 };
    };
    const annotations = (node, kind) => {
      const values = [];
      for (const range of ts.getLeadingCommentRanges(source, node.getFullStart()) || []) {
        const comment = source.slice(range.pos, range.end);
        for (const match of comment.matchAll(new RegExp('@9l-' + kind + '\\s+([^\\s*]+)', 'g'))) {
          if (!idPattern.test(match[1])) throw new AnalysisError('annotation');
          values.push(match[1]);
        }
      }
      return [...new Set(values)];
    };
    const root = expression => {
      while (ts.isPropertyAccessExpression(expression) || ts.isCallExpression(expression)) expression = expression.expression;
      return ts.isIdentifier(expression) ? expression.text : '';
    };
    const isSuite = expression => {
      while (ts.isPropertyAccessExpression(expression)) {
        if (expression.name.text === 'describe' && ts.isIdentifier(expression.expression) && tests.has(expression.expression.text)) return true;
        expression = expression.expression;
      }
      return false;
    };
    const results = [];
    // Suite scopes enclosing each recognized test, and skip/fixme modifiers
    // attributed to a suite scope. Playwright applies a suite-level modifier to
    // every test in that suite regardless of declaration order.
    const factScopes = new Map(), scopeModifiers = new Map();
    const isTestCall = (node, names) => ts.isCallExpression(node) && ts.isPropertyAccessExpression(node.expression)
      && ts.isIdentifier(node.expression.expression) && tests.has(node.expression.expression.text) && names.includes(node.expression.name.text);
    const isHookCallback = node => ts.isFunctionLike(node) && isTestCall(node.parent, ['beforeEach', 'beforeAll', 'afterEach', 'afterAll']);
    const isFunction = node => !!node && (ts.isArrowFunction(node) || ts.isFunctionExpression(node));
    const isTitle = node => !!node && (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node) || ts.isTemplateExpression(node));
    const isBoolean = node => [ts.SyntaxKind.TrueKeyword, ts.SyntaxKind.FalseKeyword].includes(node.kind)
      || (ts.isPrefixUnaryExpression(node) && node.operator === ts.SyntaxKind.ExclamationToken)
      || (ts.isBinaryExpression(node) && [ts.SyntaxKind.EqualsEqualsToken, ts.SyntaxKind.EqualsEqualsEqualsToken, ts.SyntaxKind.ExclamationEqualsToken,
        ts.SyntaxKind.ExclamationEqualsEqualsToken, ts.SyntaxKind.LessThanToken, ts.SyntaxKind.GreaterThanToken, ts.SyntaxKind.LessThanEqualsToken,
        ts.SyntaxKind.GreaterThanEqualsToken, ts.SyntaxKind.InstanceOfKeyword, ts.SyntaxKind.InKeyword].includes(node.operatorToken.kind));
    // test.skip()/test.skip(condition, reason) rather than test.skip(title, body).
    // Playwright reads a string followed by a function as a declaration at
    // runtime, so only calls that cannot be one are modifiers. Ambiguous calls
    // such as test.skip(name, run) stay declarations.
    const isModifier = node => {
      if (!isTestCall(node, ['skip', 'fixme'])) return false;
      const [first, second] = node.arguments;
      if (!first) return true;
      if (isTitle(first)) return false;
      if (isFunction(first) || isBoolean(first)) return true;
      return node.arguments.length === 1 || (node.arguments.length === 2 && isTitle(second));
    };
    // An assertion that something is absent or did not happen, which a slow
    // application also satisfies. A negated absence matcher is a presence check.
    const absence = (matcher, modifiers, args) => {
      const [first] = args;
      const is = (node, kind) => !!node && (kind === 0 ? ts.isNumericLiteral(node) && Number(node.text) === 0 : node.kind === kind);
      const disabledOption = name => !!first && ts.isObjectLiteralExpression(first) && first.properties.some(property =>
        ts.isPropertyAssignment(property) && ts.isIdentifier(property.name) && property.name.text === name && property.initializer.kind === ts.SyntaxKind.FalseKeyword);
      const emptyArray = !!first && ts.isArrayLiteralExpression(first) && first.elements.length === 0;
      const negative = ['toBeHidden', 'toBeFalsy', 'toBeNull', 'toBeUndefined'].includes(matcher)
        || (['toHaveCount', 'toHaveLength', 'toBe', 'toEqual', 'toStrictEqual'].includes(matcher) && is(first, 0))
        || (['toBe', 'toEqual', 'toStrictEqual'].includes(matcher) && is(first, ts.SyntaxKind.FalseKeyword))
        || (['toEqual', 'toStrictEqual'].includes(matcher) && emptyArray)
        || (matcher === 'toBeVisible' && disabledOption('visible')) || (matcher === 'toBeAttached' && disabledOption('attached'));
      return negative !== modifiers.includes('not');
    };
    const readsEnvironment = node => {
      if ((ts.isPropertyAccessExpression(node) && node.name.text === 'env')
        || (ts.isElementAccessExpression(node) && ts.isStringLiteralLike(node.argumentExpression) && node.argumentExpression.text === 'env')) {
        if (ts.isIdentifier(node.expression) && node.expression.text === 'process') return true;
      }
      return !!ts.forEachChild(node, child => readsEnvironment(child) || undefined);
    };
    // The condition an enclosing statement or expression applies to a modifier.
    const condition = node => ts.isIfStatement(node) || ts.isSwitchStatement(node) ? node.expression
      : ts.isConditionalExpression(node) ? node.condition : ts.isBinaryExpression(node) ? node.left : undefined;
    // The suite scope a modifier applies to, through hooks only. A modifier
    // inside any other function cannot be attributed from source and is ignored.
    // One in an after hook runs once test bodies have run, so it is reported as
    // conditional rather than disabling the suite.
    // Conditions that read process.env are reported separately: such a test
    // may never run in an environment like CI.
    const modifierScope = (node, scope) => {
      let conditional = false, environment = !!node.arguments[0] && readsEnvironment(node.arguments[0]);
      for (let ancestor = node.parent; ancestor; ancestor = ancestor.parent) {
        if (ancestor === scope) return { conditional, environment };
        if (ts.isFunctionLike(ancestor)) {
          if (!isHookCallback(ancestor)) return null;
          if (isTestCall(ancestor.parent, ['afterEach', 'afterAll'])) conditional = true;
          continue;
        }
        if (ts.isExpressionStatement(ancestor) || ts.isBlock(ancestor) || ts.isAwaitExpression(ancestor) || ts.isParenthesizedExpression(ancestor)
          || (ts.isCallExpression(ancestor) && isTestCall(ancestor, ['beforeEach', 'beforeAll', 'afterEach', 'afterAll']))) continue;
        conditional = true;
        const guard = condition(ancestor);
        if (guard && readsEnvironment(guard)) environment = true;
      }
      return null;
    };
    const bindsAlias = name => ts.isIdentifier(name) ? expects.has(name.text) || tests.has(name.text)
      : ts.isBindingPattern(name) && name.elements.some(element => ts.isBindingElement(element) && bindsAlias(element.name));
    const boundNames = name => ts.isIdentifier(name) ? [name.text]
      : ts.isBindingPattern(name) ? name.elements.flatMap(element => ts.isBindingElement(element) ? boundNames(element.name) : []) : [];
    const isDeclaration = child => (ts.isVariableDeclaration(child) || ts.isParameter(child) || ts.isFunctionDeclaration(child) || ts.isClassDeclaration(child)) && !!child.name;
    // Declarations whose names are lexically in scope at `node`, excluding file level.
    const enclosingDeclarations = node => {
      const declarations = [];
      for (let ancestor = node.parent; ancestor && ancestor !== file; ancestor = ancestor.parent) {
        if (ts.isFunctionLike(ancestor)) declarations.push(...ancestor.parameters);
        if (ts.isCatchClause(ancestor) && ancestor.variableDeclaration) declarations.push(ancestor.variableDeclaration);
        if ((ts.isForStatement(ancestor) || ts.isForOfStatement(ancestor) || ts.isForInStatement(ancestor))
          && ancestor.initializer && ts.isVariableDeclarationList(ancestor.initializer)) declarations.push(...ancestor.initializer.declarations);
        if (ts.isBlock(ancestor)) {
          for (const stmt of ancestor.statements) {
            if (ts.isVariableStatement(stmt)) declarations.push(...stmt.declarationList.declarations);
            if (ts.isFunctionDeclaration(stmt) || ts.isClassDeclaration(stmt)) declarations.push(stmt);
          }
        }
      }
      return declarations.filter(isDeclaration);
    };
    const isAsync = fn => !!fn.modifiers?.some(modifier => modifier.kind === ts.SyntaxKind.AsyncKeyword);
    // The marker must be the whole comment, or a whole JSDoc line, directly
    // before the declaration; a mention in prose does not mark anything.
    const marked = node => {
      const ranges = ts.getLeadingCommentRanges(source, node.getFullStart()) || [];
      if (!ranges.length) return false;
      const text = source.slice(ranges[ranges.length - 1].pos, ranges[ranges.length - 1].end);
      const lines = text.startsWith('//') ? [text.slice(2)] : text.replace(/^\/\*\*?|\*\/$/g, '').split('\n').map(line => line.replace(/^\s*\*?/, ''));
      return lines.some(line => line.trim() === '@9l-assertion-helper');
    };
    // A non-generator const function or function declaration with a body;
    // anything else, such as a parameter, let binding or fixture, is not
    // resolvable.
    const functionOf = declaration => {
      if (ts.isFunctionDeclaration(declaration)) return declaration.body && !declaration.asteriskToken ? declaration : null;
      if (ts.isVariableDeclaration(declaration) && ts.isIdentifier(declaration.name) && ts.isVariableDeclarationList(declaration.parent)
        && ts.isVariableStatement(declaration.parent.parent) && (declaration.parent.flags & ts.NodeFlags.Const) && declaration.initializer
        && (ts.isArrowFunction(declaration.initializer) || (ts.isFunctionExpression(declaration.initializer) && !declaration.initializer.asteriskToken))) return declaration.initializer;
      return null;
    };
    const statementOf = declaration => ts.isVariableDeclaration(declaration) ? declaration.parent.parent : declaration;
    const contains = (ancestor, node) => {
      for (let current = node; current; current = current.parent) if (current === ancestor) return true;
      return false;
    };
    // Every value binding in the file by name. A helper resolves only when its
    // name is bound exactly once, so shadowing, hoisting and block scoping
    // cannot pick the wrong declaration.
    const bindingsByName = new Map();
    (function collect(node) {
      const names = ts.isVariableDeclaration(node) || ts.isParameter(node) ? boundNames(node.name)
        : (ts.isFunctionDeclaration(node) || ts.isFunctionExpression(node) || ts.isClassDeclaration(node) || ts.isClassExpression(node)
          || ts.isEnumDeclaration(node) || ts.isModuleDeclaration(node) || ts.isImportClause(node) || ts.isImportSpecifier(node)
          || ts.isNamespaceImport(node) || ts.isImportEqualsDeclaration(node)) && node.name && ts.isIdentifier(node.name) ? [node.name.text] : [];
      for (const name of names) bindingsByName.set(name, [...(bindingsByName.get(name) || []), node]);
      ts.forEachChild(node, collect);
    })(file);
    // Imported bindings marked as assertion helpers on their import declaration.
    const importedHelpers = new Set();
    for (const statement of file.statements) {
      if (!ts.isImportDeclaration(statement) || !marked(statement) || ['@playwright/test', '@9l/playwright'].includes(statement.moduleSpecifier.text)) continue;
      const clause = statement.importClause;
      if (clause?.name) importedHelpers.add(clause.name.text);
      if (clause?.namedBindings && ts.isNamedImports(clause.namedBindings)) clause.namedBindings.elements.forEach(binding => importedHelpers.add(binding.name.text));
    }
    // A called name resolves to a marked import, or to a function declared in a
    // scope enclosing the call but outside the function being scanned (so it
    // is neither scanned twice nor called before initialization).
    const resolveHelper = (name, call, scanned) => {
      const declarations = bindingsByName.get(name) || [];
      if (declarations.length !== 1) return null;
      const [declaration] = declarations;
      if (ts.isImportClause(declaration) || ts.isImportSpecifier(declaration)) return importedHelpers.has(name) ? { marked: true, fn: null } : null;
      const fn = functionOf(declaration);
      if (!fn || !contains(statementOf(declaration).parent, call) || contains(scanned, declaration)) return null;
      return { marked: marked(statementOf(declaration)), fn };
    };
    // Inlining stops before it could exhaust the file's fact limit or its work
    // budget; the abandoned call is then an unresolved helper.
    // Inlined facts may only use what the file's own matcher and wait calls,
    // counted syntactically as an upper bound, leave of the fact limit.
    class Exhausted {}
    const budget = { facts: 0, visits: 0 };
    let direct = 0;
    (function count(node) {
      if (ts.isCallExpression(node) && ((ts.isPropertyAccessExpression(node.expression) && node.expression.name.text === 'waitForTimeout')
        || expects.has(root(node.expression)))) direct++;
      ts.forEachChild(node, count);
    })(file);
    const inlineFacts = 1984 - direct, inlineVisits = 200000;
    function visit(node, inheritedDisabled = false, scopes = [file]) {
      if (ts.isFunctionLike(node) && ts.isCallExpression(node.parent) && isSuite(node.parent.expression)) scopes = [...scopes, node];
      // A call through a test binding redeclared in an enclosing scope is not a
      // Playwright declaration; it must not add to the test inventory.
      const alias = ts.isCallExpression(node) ? root(node.expression) : '';
      if (tests.has(alias) && !enclosingDeclarations(node).some(declaration => boundNames(declaration.name).includes(alias))) {
        const expr = node.expression;
        if (isModifier(node)) {
          const scope = scopes[scopes.length - 1];
          const attributed = modifierScope(node, scope);
          const first = node.arguments[0];
          if (attributed && first?.kind !== ts.SyntaxKind.FalseKeyword) {
            const entry = scopeModifiers.get(scope) || { disabled: false, conditional: [] };
            if (!attributed.conditional && (!first || first.kind === ts.SyntaxKind.TrueKeyword)) entry.disabled = true;
            else entry.conditional.push({ ...location(node), environment: attributed.environment });
            scopeModifiers.set(scope, entry);
          }
          return;
        }
        const direct = ts.isIdentifier(expr) || (ts.isPropertyAccessExpression(expr) && ts.isIdentifier(expr.expression) && ['skip', 'only', 'fixme'].includes(expr.name.text));
        if (direct) {
          const callback = node.arguments[node.arguments.length - 1];
          const statement = ts.isExpressionStatement(node.parent) ? node.parent : node;
          const disabled = inheritedDisabled || (ts.isPropertyAccessExpression(expr) && ['skip', 'fixme'].includes(expr.name.text));
          const fact = { ...location(node), requirements: annotations(statement, 'requirement'), assertions: [], sleeps: [], conditionalSkips: [], disabled, exclusive: ts.isPropertyAccessExpression(expr) && expr.name.text === 'only', unsupported: false, limits: [] };
          // Titles are source text; include literal ones only when requested.
          if (options.titles && node.arguments[0] && (ts.isStringLiteral(node.arguments[0]) || ts.isNoSubstitutionTemplateLiteral(node.arguments[0]))) fact.title = [...node.arguments[0].text].slice(0, 200).join('');
          factScopes.set(fact, scopes);
          const limited = (code, at) => {
            fact.unsupported = true;
            // One location per reason keeps diagnostics deterministic and bounded.
            if (!fact.limits.some(limit => limit.code === code)) fact.limits.push({ code, ...location(at) });
          };
          for (let ancestor = node.parent; ancestor && ancestor !== file; ancestor = ancestor.parent) {
            const suiteCallback = ts.isFunctionLike(ancestor) && ts.isCallExpression(ancestor.parent)
              && isSuite(ancestor.parent.expression);
            if (ts.isIterationStatement(ancestor, false) || ts.isIfStatement(ancestor) || ts.isConditionalExpression(ancestor)
              || (ts.isFunctionLike(ancestor) && !suiteCallback)) limited('declaration-generation', ancestor);
          }
          if (!callback || !(ts.isArrowFunction(callback) || ts.isFunctionExpression(callback)) || !ts.isBlock(callback.body)) {
            limited('dynamic-callback', node);
          } else {
            const fixtureRoots = new Set();
            for (const parameter of callback.parameters) {
              if (!ts.isObjectBindingPattern(parameter.name)) continue;
              for (const binding of parameter.name.elements) {
                if (ts.isIdentifier(binding.name) && ['page', 'request', 'context', 'browser'].includes((binding.propertyName || binding.name).text)) fixtureRoots.add(binding.name.text);
              }
            }
            // Whether the last recognized fact was a fixed wait, in execution order
            // through resolved helpers.
            const order = { waited: false, expansions: 0 };
            // Scan one function body: the test callback, or a resolved helper whose
            // facts are attributed to the test's call site. `floating` means a
            // promise on the path from the test is neither awaited nor returned;
            // `returned` means the function's return value is consumed.
            function scanFunction(fn, roots, declarationsAt, ctx) {
              const spend = () => { if (ctx.depth > 0 && ++budget.visits > inlineVisits) throw new Exhausted(); };
              // Conservatively exclude matcher facts if an imported binding is
              // redeclared in this function or an enclosing scope.
              const shadowed = new Set();
              const declare = child => {
                if (isDeclaration(child) && bindsAlias(child.name)) {
                  limited('shadowed-binding', child);
                  boundNames(child.name).forEach(name => shadowed.add(name));
                }
              };
              function bindings(child) { spend(); declare(child); ts.forEachChild(child, bindings); }
              bindings(fn);
              enclosingDeclarations(declarationsAt).forEach(declare);
              // A fact keeps its own location. One inside an inlined helper also
              // carries the test's call site, so it is reported once per
              // location however many tests call the helper.
              const at = child => ctx.site ? { ...location(child), site: ctx.site } : location(child);
              // A callback passed to forEach has its return value discarded.
              const discards = callback => ts.isCallExpression(callback.parent) && callback.parent.arguments.includes(callback)
                && ts.isPropertyAccessExpression(callback.parent.expression) && callback.parent.expression.name.text === 'forEach';
              // This function's own concise body or return statement passes its
              // promise to the caller, which consumes it only when `returned`.
              // A nested function returns its promise to an unknown caller: not
              // unawaited, and that function is already a limit, unless the
              // caller is known to discard it.
              const returnedHere = child => {
                while (ts.isParenthesizedExpression(child.parent)) child = child.parent;
                let owner = null;
                if (ts.isArrowFunction(child.parent) && child.parent.body === child) owner = child.parent;
                else if (ts.isReturnStatement(child.parent)) {
                  owner = child.parent;
                  while (owner && !ts.isFunctionLike(owner)) owner = owner.parent;
                }
                if (!owner) return false;
                return owner === fn ? ctx.returned : !discards(owner);
              };
              // The outermost call of a then/catch/finally chain carries the
              // promise, so awaiting or returning the chain consumes the matcher.
              const chained = child => {
                while (true) {
                  while (ts.isParenthesizedExpression(child.parent)) child = child.parent;
                  if (ts.isPropertyAccessExpression(child.parent) && ['then', 'catch', 'finally'].includes(child.parent.name.text)
                    && ts.isCallExpression(child.parent.parent) && child.parent.parent.expression === child.parent) {
                    child = child.parent.parent;
                  } else return child;
                }
              };
              const consumed = child => {
                const outer = chained(child);
                return !ctx.floating && (ts.isAwaitExpression(outer.parent) || returnedHere(outer));
              };
              const record = () => { if (ctx.depth > 0 && ++budget.facts > inlineFacts) throw new Exhausted(); };
              const assertion = (child, outcomes, unawaited, isAbsence) => {
                record();
                fact.assertions.push({ ...at(child), outcomes, unawaited, absence: isAbsence, afterWait: order.waited });
                order.waited = false;
              };
              function scan(child) {
                spend();
                // Helpers, branches and shadowed identifiers prevent complete analysis.
                if (ts.isFunctionLike(child)) limited('nested-function', child);
                if (ts.isIfStatement(child) || ts.isIterationStatement(child, false) || ts.isTryStatement(child) || ts.isConditionalExpression(child) || ts.isSwitchStatement(child)
                  || (ts.isBinaryExpression(child) && [ts.SyntaxKind.AmpersandAmpersandToken, ts.SyntaxKind.BarBarToken, ts.SyntaxKind.QuestionQuestionToken].includes(child.operatorToken.kind))) limited('conditional-flow', child);
                if (ts.isCallExpression(child)) {
                  // Body-level skips can depend on runtime conditions or fixture
                  // values. Do not infer their disabled state from source alone.
                  if (tests.has(root(child.expression)) && ts.isPropertyAccessExpression(child.expression) && ['skip', 'fixme'].includes(child.expression.name.text)) limited('runtime-skip', child);
                  if (ts.isPropertyAccessExpression(child.expression) && child.expression.name.text === 'waitForTimeout') {
                    record();
                    fact.sleeps.push(at(child));
                    order.waited = true;
                  }
                  if (!expects.has(root(child.expression)) && !tests.has(root(child.expression)) && !roots.has(root(child.expression))) {
                    const helper = ts.isIdentifier(child.expression) ? resolveHelper(child.expression.text, child, fn) : null;
                    if (!helper || (!helper.marked && (ctx.depth >= 4 || ctx.stack.has(helper.fn) || order.expansions >= 64))) {
                      limited('unresolved-helper', child);
                    } else if (helper.marked) {
                      // A reviewed assertion helper counts as one attributable
                      // assertion; its body is not inferred.
                      let anchor = child;
                      while (anchor.parent && !ts.isStatement(anchor)) anchor = anchor.parent;
                      const settled = consumed(child);
                      // Only an async helper is known to return a promise; for any
                      // other unconsumed marked helper the timing is unknown.
                      const pending = !settled && !ctx.floating;
                      if (pending && !(helper.fn && isAsync(helper.fn))) limited('unknown-matcher', child);
                      assertion(child, annotations(anchor, 'outcome'), ctx.floating || (pending && !!helper.fn && isAsync(helper.fn)), false);
                    } else {
                      // Inline a same-file helper. Parameters given a browser
                      // fixture value are fixture roots inside it.
                      order.expansions++;
                      const helperRoots = new Set();
                      helper.fn.parameters.forEach((parameter, index) => {
                        const argument = child.arguments[index];
                        if (ts.isIdentifier(parameter.name) && argument && roots.has(root(argument))) helperRoots.add(parameter.name.text);
                      });
                      const settled = consumed(child);
                      const saved = { assertions: fact.assertions.length, sleeps: fact.sleeps.length, limits: [...fact.limits], unsupported: fact.unsupported, waited: order.waited, facts: budget.facts };
                      try {
                        scanFunction(helper.fn, helperRoots, helper.fn, { site: ctx.site || location(child), floating: ctx.floating || (isAsync(helper.fn) && !settled),
                          returned: settled, depth: ctx.depth + 1, stack: new Set([...ctx.stack, helper.fn]) });
                      } catch (error) {
                        if (!(error instanceof Exhausted)) throw error;
                        fact.assertions.length = saved.assertions;
                        fact.sleeps.length = saved.sleeps;
                        fact.limits = saved.limits;
                        fact.unsupported = saved.unsupported;
                        order.waited = saved.waited;
                        budget.facts = saved.facts;
                        limited('unresolved-helper', child);
                      }
                    }
                  }
                  // The matcher call, rather than expect(...), expect.poll(...) or a
                  // promise method chained after the matcher.
                  if (ts.isPropertyAccessExpression(child.expression) && expects.has(root(child.expression)) && !shadowed.has(root(child.expression))
                    && !['then', 'catch', 'finally'].includes(child.expression.name.text)) {
                    let chain = child.expression.expression;
                    const modifiers = [];
                    while (ts.isPropertyAccessExpression(chain)) { modifiers.push(chain.name.text); chain = chain.expression; }
                    if (ts.isCallExpression(chain)) {
                      let anchor = child;
                      while (anchor.parent && !ts.isStatement(anchor)) anchor = anchor.parent;
                      const awaited = consumed(child);
                      const matcher = child.expression.name.text;
                      const asyncMatcher = asyncMatchers.has(matcher) || modifiers.includes('resolves') || modifiers.includes('rejects')
                        || child.getText(file).startsWith(root(child.expression) + '.poll(');
                      // An unawaited matcher of unknown timing may be an unawaited promise.
                      if (!asyncMatcher && !syncMatchers.has(matcher) && !awaited && !ctx.floating) limited('unknown-matcher', child);
                      assertion(child, annotations(anchor, 'outcome'), ctx.floating || (asyncMatcher && !awaited), absence(matcher, modifiers, child.arguments));
                    }
                  }
                }
                ts.forEachChild(child, scan);
              }
              // Include parameters to notice an expect alias being shadowed.
              fn.parameters.forEach(scan);
              if (ts.isBlock(fn.body)) ts.forEachChild(fn.body, scan);
              else scan(fn.body);
            }
            scanFunction(callback, fixtureRoots, node, { site: null, floating: false, returned: true, depth: 0, stack: new Set() });
          }
          results.push(fact);
          if (results.length > 256) throw new AnalysisError('limit');
          return;
        }
        if (ts.isPropertyAccessExpression(expr) && ['skip', 'fixme'].includes(expr.name.text)) {
          const suite = expr.expression;
          if (ts.isPropertyAccessExpression(suite) && suite.name.text === 'describe' && ts.isIdentifier(suite.expression) && tests.has(suite.expression.text)) inheritedDisabled = true;
        }
      }
      ts.forEachChild(node, child => visit(child, inheritedDisabled, scopes));
    }
    visit(file);
    for (const fact of results) {
      for (const scope of factScopes.get(fact)) {
        const entry = scopeModifiers.get(scope);
        if (!entry) continue;
        if (entry.disabled) fact.disabled = true;
        fact.conditionalSkips.push(...entry.conditional);
      }
      if (fact.conditionalSkips.length > 16) throw new AnalysisError('limit');
    }
    // A shared modifier counts once, however many tests it applies to.
    const modifiers = new Set(results.flatMap(f => f.conditionalSkips.map(s => `${s.line}:${s.column}`)));
    if (results.reduce((n, f) => n + f.assertions.length + f.sleeps.length + f.limits.length, modifiers.size) > 2048) throw new AnalysisError('limit');
    return { version: 6, compiler: ts.version, tests: results };
  }

  return analyze;
}

async function main() {
  try {
    let analyze;
    try { analyze = createAnalyzer(require(process.argv[1])); }
    catch { throw new AnalysisError('parser-unavailable'); }
    const chunks = [];
    let bytes = 0;
    for await (const chunk of process.stdin) {
      bytes += chunk.length;
      if (bytes > 7 * 1024 * 1024) throw new AnalysisError('limit');
      chunks.push(chunk);
    }
    const request = JSON.parse(Buffer.concat(chunks).toString('utf8'));
    process.stdout.write(JSON.stringify(analyze(request.source, { titles: request.titles === true })));
  } catch (error) {
    // Diagnostics may contain literal source or credentials. Emit no raw errors.
    const code = error instanceof AnalysisError ? error.code : 'helper-failed';
    process.stdout.write(JSON.stringify({ version: 6, error: code }));
    process.exitCode = 2;
  }
}

module.exports = { createAnalyzer, main };
