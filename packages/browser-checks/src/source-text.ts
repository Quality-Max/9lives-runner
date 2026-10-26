// A lexical view of JavaScript or TypeScript source for doctor's spec checks.
// Nothing here evaluates code: it only separates code from comments, string,
// template and regex literals well enough that text inside a literal or a
// comment never reads as an import, a fixture or a config property.

// A quoted string literal: its raw contents and the index after it.
export type Literal = { text: string; end: number };
// `code` has the length and line breaks of the source, with comments and the
// contents of string, template and regex literals blanked. `literals` maps the
// index of each single- or double-quoted string's opening quote to its text.
export type Scanned = { code: string; literals: Map<number, Literal> };

// After these, a `/` starts a regex literal rather than a division.
const regexAfterChar = new Set('(,=:[!&|?{};+-*%<>~^'.split(''));
const regexAfterWord = new Set(['return', 'typeof', 'case', 'delete', 'void', 'in', 'of', 'new', 'throw', 'instanceof', 'yield', 'await', 'else', 'do']);
const blank = (text: string) => text.replace(/[^\n\r]/g, ' ');

export function scan(source: string): Scanned {
  const parts: string[] = [];
  const literals = new Map<number, Literal>();
  // Brace depth at each open template `${`, innermost last.
  const templates: number[] = [];
  let depth = 0;
  let inTemplate = false;
  let last: { kind: 'start' | 'word' | 'punct' | 'value'; text: string } = { kind: 'start', text: '' };
  let i = 0;
  const n = source.length;
  while (i < n) {
    const ch = source[i];
    const next = source[i + 1];
    if (inTemplate) {
      // Template text up to the closing backtick or the next `${`.
      let j = i;
      while (j < n && source[j] !== '`' && !(source[j] === '$' && source[j + 1] === '{')) j += source[j] === '\\' ? 2 : 1;
      j = Math.min(j, n);
      parts.push(blank(source.slice(i, j)));
      if (j >= n) { i = n; break; }
      if (source[j] === '`') { parts.push('`'); inTemplate = false; last = { kind: 'value', text: '`' }; i = j + 1; continue; }
      parts.push('${');
      templates.push(depth);
      inTemplate = false;
      last = { kind: 'punct', text: '{' };
      i = j + 2;
      continue;
    }
    if (/\s/.test(ch)) { parts.push(ch); i++; continue; }
    if (ch === '/' && next === '/') {
      let j = i;
      while (j < n && source[j] !== '\n' && source[j] !== '\r') j++;
      parts.push(blank(source.slice(i, j)));
      i = j;
      continue;
    }
    if (ch === '/' && next === '*') {
      const close = source.indexOf('*/', i + 2);
      const j = close === -1 ? n : close + 2;
      parts.push(blank(source.slice(i, j)));
      i = j;
      continue;
    }
    if (ch === "'" || ch === '"') {
      // A quoted string ends at its quote or, unterminated, at the line end,
      // so a stray quote (JSX text, say) blanks at most one line.
      let j = i + 1;
      while (j < n && source[j] !== ch && source[j] !== '\n' && source[j] !== '\r') j += source[j] === '\\' ? 2 : 1;
      j = Math.min(j, n);
      const closed = j < n && source[j] === ch;
      const end = closed ? j + 1 : j;
      literals.set(i, { text: source.slice(i + 1, j), end });
      parts.push(ch + blank(source.slice(i + 1, j)) + (closed ? ch : ''));
      last = { kind: 'value', text: ch };
      i = end;
      continue;
    }
    if (ch === '`') { parts.push('`'); inTemplate = true; i++; continue; }
    if (ch === '/' && (last.kind === 'start' || (last.kind === 'punct' && regexAfterChar.has(last.text)) || (last.kind === 'word' && regexAfterWord.has(last.text)))) {
      const end = regexEnd(source, i);
      if (end !== undefined) {
        let flags = end;
        while (flags < n && /[a-z]/i.test(source[flags])) flags++;
        parts.push('/' + blank(source.slice(i + 1, end - 1)) + '/' + source.slice(end, flags));
        last = { kind: 'value', text: '/' };
        i = flags;
        continue;
      }
    }
    if (/[A-Za-z_$]/.test(ch)) {
      let j = i + 1;
      while (j < n && /[\w$]/.test(source[j])) j++;
      const word = source.slice(i, j);
      parts.push(word);
      last = { kind: 'word', text: word };
      i = j;
      continue;
    }
    if (/[0-9]/.test(ch)) {
      let j = i + 1;
      while (j < n && /[\w.]/.test(source[j])) j++;
      parts.push(source.slice(i, j));
      last = { kind: 'value', text: '0' };
      i = j;
      continue;
    }
    if (ch === '{') depth++;
    if (ch === '}') {
      if (templates.length && templates[templates.length - 1] === depth) {
        templates.pop();
        parts.push('}');
        inTemplate = true;
        i++;
        continue;
      }
      depth--;
    }
    parts.push(ch);
    last = { kind: 'punct', text: ch };
    i++;
  }
  return { code: parts.join(''), literals };
}

// The index after a regex literal's closing slash, or undefined when the
// line ends first, in which case the `/` was not a regex.
function regexEnd(source: string, start: number): number | undefined {
  let inClass = false;
  for (let j = start + 1; j < source.length; j++) {
    const c = source[j];
    if (c === '\n' || c === '\r') return undefined;
    if (c === '\\') { j++; continue; }
    if (inClass) { if (c === ']') inClass = false; continue; }
    if (c === '[') inClass = true;
    else if (c === '/') return j === start + 1 ? undefined : j + 1;
  }
  return undefined;
}

// A local name bound by a static import or require, the module it names and
// the export it takes: '*' for a namespace or whole module, 'default' for a
// default import.
export type Binding = { local: string; module: string; imported: string };

const importForm = /(?:^|[^.\w$])import\s+([^;'"`()]{1,1024}?)\s*\bfrom\s*(['"])/g;
const requireForm = /(?:^|[^.\w$])(?:const|let|var)\s+(\{[^{}]{0,1024}\}|[A-Za-z_$][\w$]*)\s*=\s*(?:await\s+)?(?:require|import)\s*\(\s*(['"])/g;

export function importBindings(scanned: Scanned): Binding[] {
  const bindings: Binding[] = [];
  const { code, literals } = scanned;
  importForm.lastIndex = 0;
  for (let match = importForm.exec(code); match; match = importForm.exec(code)) {
    const module = literals.get(match.index + match[0].length - 1)?.text;
    const clause = match[1].trim();
    // `import type { … }` binds nothing at run time.
    if (module === undefined || /^type\s+[^\s,]/.test(clause)) continue;
    const named = clause.match(/\{([^}]*)\}/);
    for (const part of clause.replace(/\{[^}]*\}/, '').split(',').map(item => item.trim()).filter(Boolean)) {
      const namespace = part.match(/^\*\s*as\s+([A-Za-z_$][\w$]*)$/);
      if (namespace) bindings.push({ local: namespace[1], module, imported: '*' });
      else if (/^[A-Za-z_$][\w$]*$/.test(part)) bindings.push({ local: part, module, imported: 'default' });
    }
    for (const item of named ? named[1].split(',') : []) {
      const specifier = item.trim().match(/^(type\s+)?([A-Za-z_$][\w$]*)(?:\s+as\s+([A-Za-z_$][\w$]*))?$/);
      if (specifier && !specifier[1]) bindings.push({ local: specifier[3] ?? specifier[2], module, imported: specifier[2] });
    }
  }
  requireForm.lastIndex = 0;
  for (let match = requireForm.exec(code); match; match = requireForm.exec(code)) {
    const literal = literals.get(match.index + match[0].length - 1);
    const tail = literal && code.slice(literal.end, literal.end + 128).match(/^\s*\)(?:\s*\.\s*([A-Za-z_$][\w$]*))?/);
    if (!literal || !tail) continue;
    const target = match[1];
    if (!target.startsWith('{')) {
      bindings.push({ local: target, module: literal.text, imported: tail[1] ?? '*' });
      continue;
    }
    if (tail[1]) continue;
    for (const item of target.slice(1, -1).split(',')) {
      const property = item.replace(/=.*$/s, '').trim();
      const rest = property.match(/^\.\.\.\s*([A-Za-z_$][\w$]*)$/);
      if (rest) { bindings.push({ local: rest[1], module: literal.text, imported: '*' }); continue; }
      const pair = property.match(/^([A-Za-z_$][\w$]*)(?:\s*:\s*([A-Za-z_$][\w$]*))?$/);
      if (pair) bindings.push({ local: pair[2] ?? pair[1], module: literal.text, imported: pair[1] });
    }
  }
  return bindings;
}

const opens: { [key: string]: string } = { '(': ')', '[': ']', '{': '}' };
const closes = new Set([')', ']', '}']);

// The index of the bracket closing the one at `start`, within `limit` characters.
function matching(code: string, start: number, limit = 4096): number | undefined {
  let depth = 0;
  for (let j = start; j < Math.min(code.length, start + limit); j++) {
    if (opens[code[j]]) depth++;
    else if (closes.has(code[j]) && --depth === 0) return j;
  }
  return undefined;
}

// The `(` of the call whose direct argument begins at `start`, or undefined
// when the expression sits inside an object, array or anything but a call.
function enclosingCall(code: string, start: number, limit = 4096): number | undefined {
  let depth = 0;
  for (let j = start - 1; j >= Math.max(0, start - limit); j--) {
    const c = code[j];
    if (closes.has(c)) depth++;
    else if (opens[c]) {
      if (depth > 0) { depth--; continue; }
      return c === '(' ? j : undefined;
    }
  }
  return undefined;
}

// The nearest bracket that is still open at `start`, at any distance.
function enclosing(code: string, start: number): number | undefined {
  let depth = 0;
  for (let j = start - 1; j >= 0; j--) {
    if (closes.has(code[j])) depth++;
    else if (opens[code[j]]) {
      if (depth === 0) return j;
      depth--;
    }
  }
  return undefined;
}

// Whether a config may take testDir or projects from values doctor cannot
// read: a spread into an object or a call, or a call that merges several
// configs, such as `defineConfig(base, { … })` or `Object.assign`. A spread
// inside a `use: { … }` value, such as `...devices['Desktop Chrome']`, and a
// spread into an array set neither, so they do not count.
export function mergesValues(code: string): boolean {
  if (/(?<![.\w$])Object\s*\.\s*assign\s*\(/.test(code)) return true;
  for (const match of code.matchAll(/(?<![.\w$])defineConfig\s*(?:<[^<>()]{0,128}>\s*)?\(/g)) {
    const open = (match.index ?? 0) + match[0].length - 1;
    const close = matching(code, open, code.length);
    if (close === undefined || topLevel(code.slice(open + 1, close)).filter(part => part.trim()).length > 1) return true;
  }
  for (const match of code.matchAll(/\.\.\./g)) {
    const owner = enclosing(code, match.index ?? 0);
    if (owner === undefined || code[owner] === '[') continue;
    let inUse = false;
    for (let open: number | undefined = owner; open !== undefined; open = enclosing(code, open)) {
      if (code[open] === '{' && /(?<![.\w$])use\s*:\s*$/.test(code.slice(Math.max(0, open - 64), open))) { inUse = true; break; }
    }
    if (!inUse) return true;
  }
  return false;
}

// Whether `local` is called, as in `test('x', …)`, or with `members` also
// through them, as in `test.describe(…)` or `test.extend(…)`.
export function calls(code: string, local: string, members = true): boolean {
  const name = local.replace(/\$/g, '\\$');
  const chain = members ? '(?:\\.\\s*[A-Za-z_$][\\w$]*\\s*)*' : '';
  return new RegExp(`(?<![.\\w$])${name}\\s*${chain}(?:<[^<>()]{0,128}>\\s*)?\\(`).test(code);
}

// Whether `local.test` is read, as in `fixtures.test('x', …)`.
export function readsTest(code: string, local: string): boolean {
  const name = local.replace(/\$/g, '\\$');
  return new RegExp(`(?<![.\\w$])${name}\\s*\\.\\s*test(?![\\w$])`).test(code);
}

// The leading top-level arguments of the call whose `(` is at `open`, at most
// `count`. Reading stops after `limit` characters, so the last argument may be
// cut short; callers only look at how each argument starts.
function leadingArguments(code: string, open: number, count: number, limit = 4096): string[] {
  const args: string[] = [];
  let depth = 0;
  let from = open + 1;
  const end = Math.min(code.length, open + limit);
  for (let j = open + 1; j < end; j++) {
    if (opens[code[j]]) depth++;
    else if (closes.has(code[j])) {
      if (depth === 0) { args.push(code.slice(from, j)); return args; }
      depth--;
    } else if (code[j] === ',' && depth === 0) {
      args.push(code.slice(from, j));
      if (args.length === count) return args;
      from = j + 1;
    }
  }
  args.push(code.slice(from, end));
  return args;
}

// The identifier chain called by the `(` at `open`, such as `test.describe`,
// or undefined when the bracket is not a call.
function calledChain(code: string, open: number): string[] | undefined {
  const chain = code.slice(Math.max(0, open - 256), open).match(/(?<![.\w$])([A-Za-z_$][\w$]*(?:\s*\.\s*[A-Za-z_$][\w$]*)*)\s*(?:<[^<>()]{0,128}>)?\s*$/);
  return chain ? chain[1].split('.').map(segment => segment.trim()) : undefined;
}

// The brackets still open at each of `positions`, which must be ascending,
// outermost first; one pass over the code serves every position.
function openBrackets(code: string, positions: number[]): number[][] {
  const result: number[][] = [];
  const stack: number[] = [];
  for (let j = 0, next = 0; next < positions.length; j++) {
    while (next < positions.length && positions[next] <= j) { result.push([...stack]); next++; }
    if (j >= code.length) break;
    if (opens[code[j]]) stack.push(j);
    else if (closes.has(code[j])) stack.pop();
  }
  return result;
}

// Whether any of the `open` brackets is a call argument list of a test or
// hook, `test(…)`, `test.beforeEach(…)`, `test.step(…)` and the like, for a
// name in `registrars` (a local, or `<namespace>.test`), but not `describe`,
// whose callback registers tests. Playwright rejects tests and hooks
// declared in a test body, so a titled call with a callback there is a
// helper, such as a step wrapper.
function insideTestBody(code: string, open: number[], registrars: Set<string>): boolean {
  return open.some(bracket => {
    const chain = code[bracket] === '(' ? calledChain(code, bracket) : undefined;
    return Boolean(chain && (registrars.has(chain[0]) || registrars.has(chain.slice(0, 2).join('.'))) && !chain.includes('describe'));
  });
}

const titled = /^\s*['"`]/;
const callback = /^\s*(?:async\b\s*)?(?:function\b|\([\s\S]*?\)\s*(?::[^=;{}()]{0,256})?=>|[A-Za-z_$][\w$]*\s*=>)/;
// Members of Playwright's test function. These declare tests or hooks with
// any arguments; `extend` builds a test from it.
const declaring = new Set(['describe', 'beforeEach', 'afterEach', 'beforeAll', 'afterAll', 'extend']);
// These take a title and a callback when they register a test or a step.
const titledMembers = new Set(['only', 'skip', 'fixme', 'fail', 'slow', 'step']);

// Whether `local` is used the way a test function is: called with a title and
// a callback, `it('x', async ({ page }) => …)` or `it('x', { tag }, fn)`,
// outside any test or hook body; or through a member that declares tests or
// hooks or extends the test, as in `it.describe(…)`, `it.beforeEach(…)` or
// `it.extend(…)`, or `it.only('x', fn)`. A helper called any other way, such
// as `login(page)`, `login('admin', page)` or `logger.info('x')`, is not.
// `registrars` names the bindings already known to be test functions.
export function registersTests(code: string, local: string, registrars: Set<string>): boolean {
  const name = local.replace(/\$/g, '\\$');
  const use = new RegExp(`(?<![.\\w$])${name}\\s*((?:\\.\\s*[A-Za-z_$][\\w$]*\\s*)*)(?:<[^<>()]{0,128}>\\s*)?\\(`, 'g');
  // Titled calls with a callback, kept until their nesting is known.
  const titledCalls: number[] = [];
  for (const match of code.matchAll(use)) {
    const members = match[1].split('.').map(member => member.trim()).filter(Boolean);
    if (members.length && declaring.has(members[0])) return true;
    if (members.length && !titledMembers.has(members[0])) continue;
    const [title, ...rest] = leadingArguments(code, (match.index ?? 0) + match[0].length - 1, 3);
    if (titled.test(title) && rest.some(arg => callback.test(arg))) titledCalls.push(match.index ?? 0);
  }
  const nesting = new Set([...registrars, local]);
  return openBrackets(code, titledCalls).some(open => !insideTestBody(code, open, nesting));
}

// Top-level comma-separated parts of a bracket's contents.
function topLevel(text: string): string[] {
  const parts: string[] = [];
  let depth = 0;
  let from = 0;
  for (let j = 0; j < text.length; j++) {
    if (opens[text[j]]) depth++;
    else if (closes.has(text[j])) depth--;
    else if (text[j] === ',' && depth === 0) { parts.push(text.slice(from, j)); from = j + 1; }
  }
  parts.push(text.slice(from));
  return parts;
}

// The fixtures a test or hook callback destructures from its first
// parameter: `test('x', async ({ page }, testInfo) => …)`,
// `test.beforeEach(function ({ context }: Fixtures) { … })`. Callees are
// `test`, a local alias of an imported `test`, a name in `providers` (an
// import the spec uses as a test function, such as `it` from a fixtures
// module), or `<namespace>.test`, with any member such as `.beforeEach` or
// `.describe.serial`.
export function callbackFixtures(scanned: Scanned, bindings: Binding[], providers: string[] = []): Set<string> {
  const { code } = scanned;
  const tests = new Set(['test', ...bindings.filter(binding => binding.imported === 'test').map(binding => binding.local), ...providers]);
  const namespaces = new Set(bindings.filter(binding => binding.imported === '*' || binding.imported === 'default').map(binding => binding.local));
  const fixtures = new Set<string>();
  const parameters = /\(\s*\{/g;
  for (let match = parameters.exec(code); match; match = parameters.exec(code)) {
    const open = match.index;
    const close = matching(code, open);
    if (close === undefined) continue;
    // A function's parameter list is followed by `=>` or its body, with an
    // optional return type; a call such as `.toMatchObject({ page: 1 })` is not.
    if (!/^\s*(?::[^=;{}()]{0,256})?\s*(?:=>|\{)/.test(code.slice(close + 1, close + 300))) continue;
    const before = code.slice(Math.max(0, open - 256), open).match(/(?:\basync\s+function\b\s*\*?\s*[\w$]*|\bfunction\b\s*\*?\s*[\w$]*|\basync)?\s*$/);
    const start = open - (before ? before[0].length : 0);
    const call = enclosingCall(code, start);
    if (call === undefined) continue;
    const callee = code.slice(Math.max(0, call - 256), call).match(/([A-Za-z_$][\w$]*(?:\s*\.\s*[A-Za-z_$][\w$]*)*)\s*(?:<[^<>()]{0,128}>)?\s*$/);
    if (!callee) continue;
    const segments = callee[1].split('.').map(segment => segment.trim());
    if (!tests.has(segments[0]) && !(namespaces.has(segments[0]) && segments[1] === 'test')) continue;
    const first = topLevel(code.slice(open + 1, close))[0].trim();
    const pattern = first.startsWith('{') ? matching(first, 0) : undefined;
    if (pattern === undefined) continue;
    for (const property of topLevel(first.slice(1, pattern))) {
      const key = property.trim().match(/^([A-Za-z_$][\w$]*)/);
      if (key) fixtures.add(key[1]);
    }
  }
  return fixtures;
}
