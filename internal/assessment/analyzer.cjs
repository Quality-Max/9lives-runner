'use strict';

// Parse source only. Never import a spec, configuration, or application module.
const ts = require(require.resolve('typescript', { paths: [process.cwd()] }));
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
  if (file.parseDiagnostics.length) throw new Error('syntax');
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
        if (!idPattern.test(match[1])) throw new Error('annotation');
        values.push(match[1]);
      }
    }
    return [...new Set(values)];
  };
  const root = expression => {
    while (ts.isPropertyAccessExpression(expression) || ts.isCallExpression(expression)) expression = expression.expression;
    return ts.isIdentifier(expression) ? expression.text : '';
  };
  const results = [];
  const bindsAlias = name => ts.isIdentifier(name) ? expects.has(name.text) || tests.has(name.text)
    : ts.isBindingPattern(name) && name.elements.some(element => ts.isBindingElement(element) && bindsAlias(element.name));
  function visit(node, inheritedDisabled = false) {
    if (ts.isCallExpression(node) && tests.has(root(node.expression))) {
      const expr = node.expression;
      const direct = ts.isIdentifier(expr) || (ts.isPropertyAccessExpression(expr) && ts.isIdentifier(expr.expression) && ['skip', 'only', 'fixme'].includes(expr.name.text));
      if (direct) {
        const callback = node.arguments[node.arguments.length - 1];
        const statement = ts.isExpressionStatement(node.parent) ? node.parent : node;
        const disabled = inheritedDisabled || (ts.isPropertyAccessExpression(expr) && ['skip', 'fixme'].includes(expr.name.text));
        const fact = { ...location(node), requirements: annotations(statement, 'requirement'), assertions: [], sleeps: [], disabled, exclusive: ts.isPropertyAccessExpression(expr) && expr.name.text === 'only', unsupported: false };
        if (!callback || !(ts.isArrowFunction(callback) || ts.isFunctionExpression(callback)) || !ts.isBlock(callback.body) || !ts.isStringLiteralLike(node.arguments[0])) {
          fact.unsupported = true;
        } else {
          function scan(child) {
            // Helpers, branches and shadowed identifiers prevent complete analysis.
            if (ts.isFunctionLike(child) || ts.isIfStatement(child) || ts.isIterationStatement(child, false) || ts.isTryStatement(child) || ts.isConditionalExpression(child)) fact.unsupported = true;
            if ((ts.isVariableDeclaration(child) || ts.isParameter(child)) && bindsAlias(child.name)) fact.unsupported = true;
            if (ts.isCallExpression(child)) {
              // Body-level skips can depend on runtime conditions or fixture
              // values. Do not infer their disabled state from source alone.
              if (tests.has(root(child.expression)) && ts.isPropertyAccessExpression(child.expression) && ['skip', 'fixme'].includes(child.expression.name.text)) fact.unsupported = true;
              if (ts.isPropertyAccessExpression(child.expression) && child.expression.name.text === 'waitForTimeout') fact.sleeps.push(location(child));
              if (ts.isPropertyAccessExpression(child.expression) && expects.has(root(child.expression))) {
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
        if (results.length > 256) throw new Error('limit');
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
  if (results.reduce((n, f) => n + f.assertions.length + f.sleeps.length, 0) > 2048) throw new Error('limit');
  return { version: 1, compiler: ts.version, tests: results };
}

async function main() {
  try {
    const chunks = [];
    let bytes = 0;
    for await (const chunk of process.stdin) {
      bytes += chunk.length;
      if (bytes > 7 * 1024 * 1024) throw new Error('limit');
      chunks.push(chunk);
    }
    const request = JSON.parse(Buffer.concat(chunks).toString('utf8'));
    process.stdout.write(JSON.stringify(analyze(request.source)));
  } catch {
    // Diagnostics may contain literal source or credentials. Emit no raw errors.
    process.stderr.write('assessment syntax analyzer unavailable or input unsupported\n');
    process.exitCode = 2;
  }
}

module.exports = { analyze, main };
