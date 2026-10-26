"use strict";
// A lexical view of JavaScript or TypeScript source for doctor's spec checks.
// Nothing here evaluates code: it only separates code from comments, string,
// template and regex literals well enough that text inside a literal or a
// comment never reads as an import, a fixture or a config property.
Object.defineProperty(exports, "__esModule", { value: true });
exports.scan = scan;
exports.importBindings = importBindings;
exports.callbackFixtures = callbackFixtures;
// After these, a `/` starts a regex literal rather than a division.
const regexAfterChar = new Set('(,=:[!&|?{};+-*%<>~^'.split(''));
const regexAfterWord = new Set(['return', 'typeof', 'case', 'delete', 'void', 'in', 'of', 'new', 'throw', 'instanceof', 'yield', 'await', 'else', 'do']);
const blank = (text) => text.replace(/[^\n\r]/g, ' ');
function scan(source) {
    const parts = [];
    const literals = new Map();
    // Brace depth at each open template `${`, innermost last.
    const templates = [];
    let depth = 0;
    let inTemplate = false;
    let last = { kind: 'start', text: '' };
    let i = 0;
    const n = source.length;
    while (i < n) {
        const ch = source[i];
        const next = source[i + 1];
        if (inTemplate) {
            // Template text up to the closing backtick or the next `${`.
            let j = i;
            while (j < n && source[j] !== '`' && !(source[j] === '$' && source[j + 1] === '{'))
                j += source[j] === '\\' ? 2 : 1;
            j = Math.min(j, n);
            parts.push(blank(source.slice(i, j)));
            if (j >= n) {
                i = n;
                break;
            }
            if (source[j] === '`') {
                parts.push('`');
                inTemplate = false;
                last = { kind: 'value', text: '`' };
                i = j + 1;
                continue;
            }
            parts.push('${');
            templates.push(depth);
            inTemplate = false;
            last = { kind: 'punct', text: '{' };
            i = j + 2;
            continue;
        }
        if (/\s/.test(ch)) {
            parts.push(ch);
            i++;
            continue;
        }
        if (ch === '/' && next === '/') {
            let j = i;
            while (j < n && source[j] !== '\n' && source[j] !== '\r')
                j++;
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
            while (j < n && source[j] !== ch && source[j] !== '\n' && source[j] !== '\r')
                j += source[j] === '\\' ? 2 : 1;
            j = Math.min(j, n);
            const closed = j < n && source[j] === ch;
            const end = closed ? j + 1 : j;
            literals.set(i, { text: source.slice(i + 1, j), end });
            parts.push(ch + blank(source.slice(i + 1, j)) + (closed ? ch : ''));
            last = { kind: 'value', text: ch };
            i = end;
            continue;
        }
        if (ch === '`') {
            parts.push('`');
            inTemplate = true;
            i++;
            continue;
        }
        if (ch === '/' && (last.kind === 'start' || (last.kind === 'punct' && regexAfterChar.has(last.text)) || (last.kind === 'word' && regexAfterWord.has(last.text)))) {
            const end = regexEnd(source, i);
            if (end !== undefined) {
                let flags = end;
                while (flags < n && /[a-z]/i.test(source[flags]))
                    flags++;
                parts.push('/' + blank(source.slice(i + 1, end - 1)) + '/' + source.slice(end, flags));
                last = { kind: 'value', text: '/' };
                i = flags;
                continue;
            }
        }
        if (/[A-Za-z_$]/.test(ch)) {
            let j = i + 1;
            while (j < n && /[\w$]/.test(source[j]))
                j++;
            const word = source.slice(i, j);
            parts.push(word);
            last = { kind: 'word', text: word };
            i = j;
            continue;
        }
        if (/[0-9]/.test(ch)) {
            let j = i + 1;
            while (j < n && /[\w.]/.test(source[j]))
                j++;
            parts.push(source.slice(i, j));
            last = { kind: 'value', text: '0' };
            i = j;
            continue;
        }
        if (ch === '{')
            depth++;
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
function regexEnd(source, start) {
    let inClass = false;
    for (let j = start + 1; j < source.length; j++) {
        const c = source[j];
        if (c === '\n' || c === '\r')
            return undefined;
        if (c === '\\') {
            j++;
            continue;
        }
        if (inClass) {
            if (c === ']')
                inClass = false;
            continue;
        }
        if (c === '[')
            inClass = true;
        else if (c === '/')
            return j === start + 1 ? undefined : j + 1;
    }
    return undefined;
}
const importForm = /(?:^|[^.\w$])import\s+([^;'"`()]{1,1024}?)\s*\bfrom\s*(['"])/g;
const requireForm = /(?:^|[^.\w$])(?:const|let|var)\s+(\{[^{}]{0,1024}\}|[A-Za-z_$][\w$]*)\s*=\s*(?:await\s+)?(?:require|import)\s*\(\s*(['"])/g;
function importBindings(scanned) {
    const bindings = [];
    const { code, literals } = scanned;
    importForm.lastIndex = 0;
    for (let match = importForm.exec(code); match; match = importForm.exec(code)) {
        const module = literals.get(match.index + match[0].length - 1)?.text;
        const clause = match[1].trim();
        // `import type { … }` binds nothing at run time.
        if (module === undefined || /^type\s+[^\s,]/.test(clause))
            continue;
        const named = clause.match(/\{([^}]*)\}/);
        for (const part of clause.replace(/\{[^}]*\}/, '').split(',').map(item => item.trim()).filter(Boolean)) {
            const namespace = part.match(/^\*\s*as\s+([A-Za-z_$][\w$]*)$/);
            if (namespace)
                bindings.push({ local: namespace[1], module, imported: '*' });
            else if (/^[A-Za-z_$][\w$]*$/.test(part))
                bindings.push({ local: part, module, imported: 'default' });
        }
        for (const item of named ? named[1].split(',') : []) {
            const specifier = item.trim().match(/^(type\s+)?([A-Za-z_$][\w$]*)(?:\s+as\s+([A-Za-z_$][\w$]*))?$/);
            if (specifier && !specifier[1])
                bindings.push({ local: specifier[3] ?? specifier[2], module, imported: specifier[2] });
        }
    }
    requireForm.lastIndex = 0;
    for (let match = requireForm.exec(code); match; match = requireForm.exec(code)) {
        const literal = literals.get(match.index + match[0].length - 1);
        const tail = literal && code.slice(literal.end, literal.end + 128).match(/^\s*\)(?:\s*\.\s*([A-Za-z_$][\w$]*))?/);
        if (!literal || !tail)
            continue;
        const target = match[1];
        if (!target.startsWith('{')) {
            bindings.push({ local: target, module: literal.text, imported: tail[1] ?? '*' });
            continue;
        }
        if (tail[1])
            continue;
        for (const item of target.slice(1, -1).split(',')) {
            const property = item.replace(/=.*$/s, '').trim();
            const rest = property.match(/^\.\.\.\s*([A-Za-z_$][\w$]*)$/);
            if (rest) {
                bindings.push({ local: rest[1], module: literal.text, imported: '*' });
                continue;
            }
            const pair = property.match(/^([A-Za-z_$][\w$]*)(?:\s*:\s*([A-Za-z_$][\w$]*))?$/);
            if (pair)
                bindings.push({ local: pair[2] ?? pair[1], module: literal.text, imported: pair[1] });
        }
    }
    return bindings;
}
const opens = { '(': ')', '[': ']', '{': '}' };
const closes = new Set([')', ']', '}']);
// The index of the bracket closing the one at `start`, within `limit` characters.
function matching(code, start, limit = 4096) {
    let depth = 0;
    for (let j = start; j < Math.min(code.length, start + limit); j++) {
        if (opens[code[j]])
            depth++;
        else if (closes.has(code[j]) && --depth === 0)
            return j;
    }
    return undefined;
}
// The `(` of the call whose direct argument begins at `start`, or undefined
// when the expression sits inside an object, array or anything but a call.
function enclosingCall(code, start, limit = 4096) {
    let depth = 0;
    for (let j = start - 1; j >= Math.max(0, start - limit); j--) {
        const c = code[j];
        if (closes.has(c))
            depth++;
        else if (opens[c]) {
            if (depth > 0) {
                depth--;
                continue;
            }
            return c === '(' ? j : undefined;
        }
    }
    return undefined;
}
// Top-level comma-separated parts of a bracket's contents.
function topLevel(text) {
    const parts = [];
    let depth = 0;
    let from = 0;
    for (let j = 0; j < text.length; j++) {
        if (opens[text[j]])
            depth++;
        else if (closes.has(text[j]))
            depth--;
        else if (text[j] === ',' && depth === 0) {
            parts.push(text.slice(from, j));
            from = j + 1;
        }
    }
    parts.push(text.slice(from));
    return parts;
}
// The fixtures a test or hook callback destructures from its first
// parameter: `test('x', async ({ page }, testInfo) => …)`,
// `test.beforeEach(function ({ context }: Fixtures) { … })`. Callees are
// `test`, a local alias of an imported `test`, or `<namespace>.test`, with any
// member such as `.beforeEach` or `.describe.serial`.
function callbackFixtures(scanned, bindings) {
    const { code } = scanned;
    const tests = new Set(['test', ...bindings.filter(binding => binding.imported === 'test').map(binding => binding.local)]);
    const namespaces = new Set(bindings.filter(binding => binding.imported === '*' || binding.imported === 'default').map(binding => binding.local));
    const fixtures = new Set();
    const parameters = /\(\s*\{/g;
    for (let match = parameters.exec(code); match; match = parameters.exec(code)) {
        const open = match.index;
        const close = matching(code, open);
        if (close === undefined)
            continue;
        // A function's parameter list is followed by `=>` or its body, with an
        // optional return type; a call such as `.toMatchObject({ page: 1 })` is not.
        if (!/^\s*(?::[^=;{}()]{0,256})?\s*(?:=>|\{)/.test(code.slice(close + 1, close + 300)))
            continue;
        const before = code.slice(Math.max(0, open - 256), open).match(/(?:\basync\s+function\b\s*\*?\s*[\w$]*|\bfunction\b\s*\*?\s*[\w$]*|\basync)?\s*$/);
        const start = open - (before ? before[0].length : 0);
        const call = enclosingCall(code, start);
        if (call === undefined)
            continue;
        const callee = code.slice(Math.max(0, call - 256), call).match(/([A-Za-z_$][\w$]*(?:\s*\.\s*[A-Za-z_$][\w$]*)*)\s*(?:<[^<>()]{0,128}>)?\s*$/);
        if (!callee)
            continue;
        const segments = callee[1].split('.').map(segment => segment.trim());
        if (!tests.has(segments[0]) && !(namespaces.has(segments[0]) && segments[1] === 'test'))
            continue;
        const first = topLevel(code.slice(open + 1, close))[0].trim();
        const pattern = first.startsWith('{') ? matching(first, 0) : undefined;
        if (pattern === undefined)
            continue;
        for (const property of topLevel(first.slice(1, pattern))) {
            const key = property.trim().match(/^([A-Za-z_$][\w$]*)/);
            if (key)
                fixtures.add(key[1]);
        }
    }
    return fixtures;
}
