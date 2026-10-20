// Fails a pull request whose branch name, title, body, commit messages, added
// file paths or added lines contain a blocked term. The terms come from the
// BLOCKED_TERMS secret (one per line or comma-separated) so this public
// repository never lists them. Output names only where a term was found,
// never the term or the matching text.
const {execFileSync} = require('node:child_process');
const fs = require('node:fs');

// Lowercase ASCII letters and digits only, so "Foo Bar", "foo-bar", "foo_bar"
// and "FooBar" all compare equal and accents do not hide a match.
function normalize(text) {
  return String(text).normalize('NFKD').replace(/[̀-ͯ]/g, '').toLowerCase().replace(/[^a-z0-9]/g, '');
}

function parseTerms(raw) {
  return [...new Set(String(raw || '').split(/[\n,]/).map(normalize).filter(Boolean))];
}

function containsTerm(text, terms) {
  const value = normalize(text);
  return terms.some(term => value.includes(term));
}

// Labels of the sources that contain a blocked term, in order.
function findBlocked(sources, terms) {
  return sources.filter(source => containsTerm(source.text, terms)).map(source => source.label);
}

// Added lines grouped by destination path from `git diff -U0` output.
function addedLines(diff) {
  const files = new Map();
  let current;
  let header = false;
  for (const line of diff.split('\n')) {
    if (line.startsWith('diff --git ')) {
      header = true;
      current = undefined;
    } else if (header && line.startsWith('@@')) {
      header = false;
    } else if (header && line.startsWith('+++ ')) {
      current = line === '+++ /dev/null' ? undefined : line.replace(/^\+\+\+ "?b\//, '').replace(/"$/, '');
      if (current !== undefined && !files.has(current)) files.set(current, []);
    } else if (!header && current !== undefined && line.startsWith('+')) {
      files.get(current).push(line.slice(1));
    }
  }
  return files;
}

function git(args) {
  return execFileSync('git', args, {encoding: 'utf8', maxBuffer: 512 * 1024 * 1024});
}

// Every text a pull request publishes, labelled without repeating its content.
function pullRequestSources(pr, head) {
  const sources = [
    {label: 'branch name', text: pr.head.ref},
    {label: 'title', text: pr.title},
    {label: 'description', text: pr.body || ''},
  ];
  const base = git(['merge-base', pr.base.sha, head]).trim();
  for (const record of git(['log', '--format=%H%x00%B%x1e', `${base}..${head}`]).split('\x1e')) {
    const [sha, message] = record.replace(/^\n/, '').split('\0');
    if (sha) sources.push({label: `commit ${sha.slice(0, 7)} message`, text: message});
  }
  const files = addedLines(git(['-c', 'core.quotePath=false', 'diff', '--no-color', '--no-ext-diff', '-U0', '-M', base, head]));
  let index = 0;
  for (const [file, lines] of files) {
    index += 1;
    sources.push({label: `changed file #${index} path`, text: file});
    sources.push({label: `added lines in changed file #${index}`, text: lines.join('\n')});
  }
  return sources;
}

function main() {
  const terms = parseTerms(process.env.BLOCKED_TERMS);
  if (terms.length === 0) {
    console.error('BLOCKED_TERMS is not configured; refusing to pass the check.');
    return 1;
  }
  const event = JSON.parse(fs.readFileSync(process.env.GITHUB_EVENT_PATH, 'utf8'));
  const pr = event.pull_request;
  const head = process.env.PR_HEAD || pr.head.sha;
  const found = findBlocked(pullRequestSources(pr, head), terms);
  if (found.length === 0) {
    console.log('No blocked terms found.');
    return 0;
  }
  console.error('Blocked terms found in:');
  for (const label of found) console.error(`- ${label}`);
  console.error('Remove them, rename the branch or rewrite the commits, then push again.');
  return 1;
}

if (require.main === module) process.exitCode = main();

module.exports = {addedLines, containsTerm, findBlocked, normalize, parseTerms};
