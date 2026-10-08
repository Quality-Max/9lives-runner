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

  function analyze(source) {
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
    function visit(node, inheritedDisabled = false) {
      // A call through a test binding redeclared in an enclosing scope is not a
      // Playwright declaration; it must not add to the test inventory.
      const alias = ts.isCallExpression(node) ? root(node.expression) : '';
      if (tests.has(alias) && !enclosingDeclarations(node).some(declaration => boundNames(declaration.name).includes(alias))) {
        const expr = node.expression;
        const direct = ts.isIdentifier(expr) || (ts.isPropertyAccessExpression(expr) && ts.isIdentifier(expr.expression) && ['skip', 'only', 'fixme'].includes(expr.name.text));
        if (direct) {
          const callback = node.arguments[node.arguments.length - 1];
          const statement = ts.isExpressionStatement(node.parent) ? node.parent : node;
          const disabled = inheritedDisabled || (ts.isPropertyAccessExpression(expr) && ['skip', 'fixme'].includes(expr.name.text));
          const fact = { ...location(node), requirements: annotations(statement, 'requirement'), assertions: [], sleeps: [], disabled, exclusive: ts.isPropertyAccessExpression(expr) && expr.name.text === 'only', unsupported: false, limits: [] };
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
            // Conservatively exclude matcher facts if an imported binding is
            // redeclared anywhere in this callback or an enclosing scope.
            const shadowed = new Set();
            const declare = child => {
              if (isDeclaration(child) && bindsAlias(child.name)) {
                limited('shadowed-binding', child);
                boundNames(child.name).forEach(name => shadowed.add(name));
              }
            };
            function bindings(child) { declare(child); ts.forEachChild(child, bindings); }
            bindings(callback);
            enclosingDeclarations(node).forEach(declare);
            function scan(child) {
              // Helpers, branches and shadowed identifiers prevent complete analysis.
              if (ts.isFunctionLike(child)) limited('nested-function', child);
              if (ts.isIfStatement(child) || ts.isIterationStatement(child, false) || ts.isTryStatement(child) || ts.isConditionalExpression(child) || ts.isSwitchStatement(child)
                || (ts.isBinaryExpression(child) && [ts.SyntaxKind.AmpersandAmpersandToken, ts.SyntaxKind.BarBarToken, ts.SyntaxKind.QuestionQuestionToken].includes(child.operatorToken.kind))) limited('conditional-flow', child);
              if (ts.isCallExpression(child)) {
                // Body-level skips can depend on runtime conditions or fixture
                // values. Do not infer their disabled state from source alone.
                if (tests.has(root(child.expression)) && ts.isPropertyAccessExpression(child.expression) && ['skip', 'fixme'].includes(child.expression.name.text)) limited('runtime-skip', child);
                if (ts.isPropertyAccessExpression(child.expression) && child.expression.name.text === 'waitForTimeout') fact.sleeps.push(location(child));
                if (!expects.has(root(child.expression)) && !tests.has(root(child.expression)) && !fixtureRoots.has(root(child.expression))) limited('unresolved-helper', child);
                if (ts.isPropertyAccessExpression(child.expression) && expects.has(root(child.expression)) && !shadowed.has(root(child.expression))) {
                  // The outer matcher call, rather than expect(...) or expect.poll(...).
                  let chain = child.expression.expression;
                  while (ts.isPropertyAccessExpression(chain)) chain = chain.expression;
                  if (ts.isCallExpression(chain)) {
                    let anchor = child;
                    while (anchor.parent && !ts.isStatement(anchor)) anchor = anchor.parent;
                    const awaited = ts.isAwaitExpression(child.parent) || ts.isReturnStatement(child.parent);
                    const asyncMatcher = asyncMatchers.has(child.expression.name.text) || child.getText(file).startsWith(root(child.expression) + '.poll(');
                    fact.assertions.push({ ...location(child), outcomes: annotations(anchor, 'outcome'), unawaited: asyncMatcher && !awaited });
                  }
                }
              }
              ts.forEachChild(child, scan);
            }
            // Include callback parameters to notice an expect alias being shadowed.
            callback.parameters.forEach(scan);
            ts.forEachChild(callback.body, scan);
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
      ts.forEachChild(node, child => visit(child, inheritedDisabled));
    }
    visit(file);
    if (results.reduce((n, f) => n + f.assertions.length + f.sleeps.length + f.limits.length, 0) > 2048) throw new AnalysisError('limit');
    return { version: 2, compiler: ts.version, tests: results };
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
    process.stdout.write(JSON.stringify(analyze(request.source)));
  } catch (error) {
    // Diagnostics may contain literal source or credentials. Emit no raw errors.
    const code = error instanceof AnalysisError ? error.code : 'helper-failed';
    process.stdout.write(JSON.stringify({ version: 2, error: code }));
    process.exitCode = 2;
  }
}

module.exports = { createAnalyzer, main };
