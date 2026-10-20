const assert = require('node:assert/strict');
const test = require('node:test');
const {addedLines, containsTerm, findBlocked, parseTerms} = require('./check-blocked-terms.cjs');

test('terms are split on lines and commas and normalized', () => {
  assert.deepEqual(parseTerms('Acme Corp\n, jane-doe,\n\nACME_CORP '), ['acmecorp', 'janedoe']);
  assert.deepEqual(parseTerms(undefined), []);
});

test('separators, case and accents do not hide a term', () => {
  const terms = parseTerms('acme corp,jane doe');
  for (const text of ['ACME-corp', 'feature/acme_corp-notes', 'AcmeCorp', 'Jáne Döe']) {
    assert.ok(containsTerm(text, terms), text);
  }
  assert.ok(!containsTerm('acme company', terms));
});

test('findings name the source, never the matching text', () => {
  const terms = parseTerms('jane doe');
  const found = findBlocked([
    {label: 'branch name', text: 'user/jane-doe-plan'},
    {label: 'title', text: 'Add target contract'},
    {label: 'commit abc1234 message', text: 'Merge into user/jane-doe-plan'},
  ], terms);
  assert.deepEqual(found, ['branch name', 'commit abc1234 message']);
});

test('only added lines are attributed to their destination file', () => {
  const diff = [
    'diff --git a/a.md b/a.md',
    '--- a/a.md',
    '+++ b/a.md',
    '@@ -1 +1 @@',
    '-removed line',
    '+added line',
    'diff --git a/gone.md b/gone.md',
    '--- a/gone.md',
    '+++ /dev/null',
    '@@ -1 +0,0 @@',
    '-deleted file',
    'diff --git a/new.md b/new.md',
    'new file mode 100644',
    '--- /dev/null',
    '+++ b/new.md',
    '@@ -0,0 +1,2 @@',
    '+first',
    '+++ b/looks-like-a-header',
  ].join('\n');
  assert.deepEqual([...addedLines(diff)], [['a.md', ['added line']], ['new.md', ['first', '++ b/looks-like-a-header']]]);
});
