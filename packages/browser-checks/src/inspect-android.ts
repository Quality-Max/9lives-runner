// `9l inspect --android`: how testable the screen on an attached Android
// device is. One read-only `uiautomator dump` of the active window through
// adb; nothing is installed, tapped, typed or written to the device. Each
// control is ranked by the handle the 9l Android driver would find it with:
// a unique resource ID (`app.getByTestId`), a unique text or description
// alias in the driver's `controls` option, or nothing but its position.
//
// The matching here mirrors the driver: resource IDs match the bare name or
// `<package>:id/<name>`, aliases match exact text, description or hint with an
// optional class, and every match is package-scoped and must be unique among
// all nodes, not only the tappable ones.
import { spawn } from 'node:child_process';
import { androidDefaults, androidToolPaths } from './android-env';
import { cell, codeSpan, meter } from './report';

export type InspectAndroidArgs = {
  /** adb serial; required when more than one device is attached. */
  serial?: string;
  /** Only report controls of this package. */
  package?: string;
  timeoutMs?: number;
};

export type AndroidNode = {
  resourceId: string;
  className: string;
  package: string;
  text: string;
  description: string;
  hint: string;
  clickable: boolean;
  longClickable: boolean;
  checkable: boolean;
  enabled: boolean;
  password: boolean;
  bounds: string;
  /** Index of the enclosing node, or -1 at the root. */
  parent: number;
};

/** `stable`: a resource ID no other node has. `repeated`: a resource ID other
 * nodes share, so `getByTestId` fails as ambiguous while they are on screen.
 * `copy`: no usable ID, but a unique text, description or hint alias that
 * breaks when the copy or the device language changes. `none`: only
 * coordinates reach it. */
export type NativeStability = 'stable' | 'repeated' | 'copy' | 'none';

export type AndroidControlReport = {
  className: string;
  package: string;
  /** What a user perceives: text, description or hint, bounded. Empty when it has none. */
  label: string;
  resourceId?: string;
  bounds: string;
  stability: NativeStability;
  /** The 9l handle: a getByTestId call, or a `controls` alias object. */
  locator?: string;
  note?: string;
};

export type AndroidTestability = {
  controls: number; stable: number; repeated: number; copy: number; none: number; score: number; note?: string;
};

export type InspectAndroidResult = {
  platform: 'android';
  packages: string[];
  nodeCount: number;
  controls: AndroidControlReport[];
  truncated?: number;
  testability: AndroidTestability;
  warnings: string[];
};

const maxXmlBytes = 8 << 20;
const maxControls = 300;
const maxLabel = 80;
const maxAliasValue = 200;
const serialPattern = /^[A-Za-z0-9._:-]{1,128}$/;
const packagePattern = /^[A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z0-9_]+)+$/;
const editableClasses = new Set(['android.widget.EditText', 'android.widget.AutoCompleteTextView', 'android.widget.MultiAutoCompleteTextView']);
const testIdPattern = /^[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,127}$/;

const entities: Record<string, string> = { amp: '&', lt: '<', gt: '>', quot: '"', apos: "'" };
function decode(value: string): string {
  return value.replace(/&(#x[0-9a-fA-F]+|#[0-9]+|amp|lt|gt|quot|apos);/g, (_, name: string) => {
    if (name[0] !== '#') return entities[name];
    const code = name[1] === 'x' ? Number.parseInt(name.slice(2), 16) : Number.parseInt(name.slice(1), 10);
    return code <= 0x10ffff ? String.fromCodePoint(code) : '';
  });
}

/** The nodes of a UiAutomator window dump, in document order. Only `node`
 * elements are read; their attribute values are double-quoted and escaped. */
export function parseUiHierarchy(xml: string): AndroidNode[] {
  if (!/<hierarchy\b/.test(xml)) throw new Error('the device returned no UI hierarchy');
  const nodes: AndroidNode[] = [];
  const open: number[] = [];
  for (const tag of xml.matchAll(/<node((?:\s+[\w:-]+="[^"]*")*)\s*(\/?)>|<\/node\s*>/g)) {
    if (tag[1] === undefined) { open.pop(); continue; }
    const attributes = new Map<string, string>();
    for (const [, name, value] of tag[1].matchAll(/([\w:-]+)="([^"]*)"/g)) attributes.set(name, decode(value));
    const text = (name: string) => attributes.get(name) ?? '';
    const flag = (name: string) => attributes.get(name) === 'true';
    nodes.push({
      resourceId: text('resource-id'), className: text('class'), package: text('package'),
      text: text('text'), description: text('content-desc'), hint: text('hint'),
      clickable: flag('clickable'), longClickable: flag('long-clickable'), checkable: flag('checkable'),
      enabled: flag('enabled'), password: flag('password'), bounds: text('bounds'), parent: open.at(-1) ?? -1,
    });
    if (tag[2] !== '/') open.push(nodes.length - 1);
  }
  return nodes;
}

const interactive = (node: AndroidNode) => node.clickable || node.longClickable || node.checkable || editableClasses.has(node.className);

/** The test ID the driver would use for a resource ID: the bare name, or the
 * entry name of `<package>:id/<name>`. Another package's resource is not reachable. */
function testIdOf(node: AndroidNode): string | undefined {
  const prefix = `${node.package}:id/`;
  const id = node.resourceId.startsWith(prefix) ? node.resourceId.slice(prefix.length) : node.resourceId.includes(':id/') ? '' : node.resourceId;
  return id && testIdPattern.test(id) ? id : undefined;
}

const short = (value: string) => {
  const flat = value.replace(/\s*\n\s*/g, ' ⏎ ').replace(/\s+/g, ' ').trim();
  return flat.length > maxLabel ? `${flat.slice(0, maxLabel - 1)}…` : flat;
};
const literal = (value: string) => JSON.stringify(value);

const idFix = 'Give it a test ID: Semantics(identifier:) in Flutter, Modifier.testTag with testTagsAsResourceId in Compose, android:id in Views.';

/** Rank every control of a dump by the handle 9l would find it with. */
export function inspectAndroidHierarchy(xml: string, options: { package?: string } = {}): InspectAndroidResult {
  const all = parseUiHierarchy(xml);
  const nodes = options.package ? all.filter((node) => node.package === options.package) : all;
  const packages = [...new Set(all.map((node) => node.package).filter(Boolean))].sort();
  const count = (predicate: (node: AndroidNode) => boolean, pkg: string) =>
    nodes.reduce((total, node) => total + (node.package === pkg && predicate(node) ? 1 : 0), 0);

  const children = all.map(() => [] as number[]);
  all.forEach((node, index) => { if (node.parent >= 0) children[node.parent].push(index); });
  // Labels inside a control that are not controls themselves: a native
  // button often carries its text on a child TextView, and a tap on the
  // child lands on the control.
  const innerLabels = (index: number): AndroidNode[] => children[index].flatMap((child) =>
    interactive(all[child]) ? [] : [...((all[child].text || all[child].description) ? [all[child]] : []), ...innerLabels(child)]);

  const controls: AndroidControlReport[] = [];
  for (const [index, node] of all.entries()) {
    if (!interactive(node) || (options.package && node.package !== options.package)) continue;
    const inner = innerLabels(index);
    // A field's text is what was typed into it, possibly a password: never
    // a label, never reported.
    const ownText = editableClasses.has(node.className) || node.password ? '' : node.text;
    const label = short(ownText || node.description || node.hint || inner.map((child) => child.text || child.description).join(' ⏎ '));
    const base = { className: node.className, package: node.package, label, bounds: node.bounds };
    const id = testIdOf(node);
    if (id) {
      const shared = count((other) => testIdOf(other) === id, node.package);
      controls.push(shared === 1
        ? { ...base, resourceId: id, stability: 'stable', locator: `app.getByTestId(${literal(id)})` }
        : { ...base, resourceId: id, stability: 'repeated', locator: `app.getByTestId(${literal(id)})`,
          note: `${shared} nodes on this screen share this ID, so getByTestId fails as ambiguous while they are all shown. Give each instance its own ID, for example with the item's key as a suffix.` });
      continue;
    }
    // The aliases the driver accepts, most specific value first; a backslash
    // is refused in text and description aliases.
    const candidates: Array<[string, string, (other: AndroidNode) => string, string?]> = [];
    if (ownText && !ownText.includes('\\')) candidates.push(['text', ownText, (other) => other.text]);
    if (node.description && !node.description.includes('\\')) candidates.push(['accessibilityId', node.description, (other) => other.description]);
    if (node.hint) candidates.push(['hint', node.hint, (other) => other.hint]);
    for (const child of inner) {
      if (child.text && !child.text.includes('\\')) candidates.push(['text', child.text, (other) => other.text, child.className]);
      if (child.description && !child.description.includes('\\')) candidates.push(['accessibilityId', child.description, (other) => other.description, child.className]);
    }
    let alias: string | undefined;
    for (const [matcher, value, read, className = node.className] of candidates) {
      // An alias must quote the whole value; a long merged label is not a useful handle to print.
      if (value.length > maxAliasValue) continue;
      if (count((other) => read(other) === value, node.package) === 1) { alias = `{${matcher}: ${literal(value)}}`; break; }
      if (count((other) => read(other) === value && other.className === className, node.package) === 1) {
        alias = `{${matcher}: ${literal(value)}, className: ${literal(className)}}`; break;
      }
    }
    if (alias) {
      controls.push({ ...base, stability: 'copy', locator: alias,
        note: `Reachable only through a controls alias on its ${alias.startsWith('{text') ? 'text' : alias.startsWith('{hint') ? 'hint' : 'accessibility label'}, which breaks when the copy or the device language changes. ${idFix}` });
    } else {
      controls.push({ ...base, stability: 'none',
        note: `${candidates.length ? 'Its label is shared with other nodes of the same class, so no alias resolves to it alone' : 'It has no ID, text, description or hint'}; a test can reach it only by coordinates (tapAt), which move with layout, font size and screen size. ${idFix}` });
    }
  }

  const tally = (value: NativeStability) => controls.filter((control) => control.stability === value).length;
  const testability: AndroidTestability = {
    controls: controls.length, stable: tally('stable'), repeated: tally('repeated'), copy: tally('copy'), none: tally('none'),
    score: controls.length === 0 ? 100 : Math.round((tally('stable') / controls.length) * 100),
  };
  const missing = controls.length - testability.stable;
  if (missing > 0) {
    testability.note = `${missing} of ${controls.length} controls have no unique test ID: ` +
      [testability.repeated && `${testability.repeated} share an ID`, testability.copy && `${testability.copy} are reachable only by their copy`,
        testability.none && `${testability.none} only by coordinates`].filter(Boolean).join(', ') + '.';
  }

  const warnings: string[] = [];
  if (options.package && nodes.length === 0) warnings.push(`No node of ${options.package} is on the active window; it shows ${packages.join(', ') || 'nothing'}.`);
  else if (controls.length === 0) warnings.push(`No tappable, checkable or editable controls in ${nodes.length} nodes. The screen may still be loading, or its controls may not be exposed to accessibility.`);
  if (nodes.some((node) => node.password)) warnings.push('A password field is on screen; its value is never reported.');

  const order: Record<NativeStability, number> = { none: 0, copy: 1, repeated: 2, stable: 3 };
  const ranked = controls.map((control, index) => ({ control, index }))
    .sort((a, b) => order[a.control.stability] - order[b.control.stability] || a.index - b.index).map(({ control }) => control);
  const shown = ranked.slice(0, maxControls);
  return {
    platform: 'android', packages, nodeCount: nodes.length, controls: shown,
    ...(ranked.length > shown.length ? { truncated: ranked.length - shown.length } : {}),
    testability, warnings,
  };
}

type Exec = (file: string, args: string[], timeoutMs: number) => Promise<{ status: number | null; stdout: string; stderr: string }>;

const exec: Exec = (file, args, timeoutMs) => new Promise((settle, reject) => {
  const child = spawn(file, args, { stdio: ['ignore', 'pipe', 'pipe'], windowsHide: true });
  const out: Buffer[] = [], err: Buffer[] = [];
  let size = 0;
  const timer = setTimeout(() => { child.kill('SIGKILL'); reject(new Error(`adb did not answer within ${Math.round(timeoutMs / 1000)}s`)); }, timeoutMs);
  child.stdout.on('data', (chunk: Buffer) => {
    size += chunk.length;
    if (size > maxXmlBytes) { child.kill('SIGKILL'); clearTimeout(timer); reject(new Error('the UI hierarchy exceeds 8 MiB')); } else out.push(chunk);
  });
  child.stderr.on('data', (chunk: Buffer) => { if (err.length < 64) err.push(chunk); });
  child.on('error', (error: NodeJS.ErrnoException) => {
    clearTimeout(timer);
    reject(new Error(error.code === 'ENOENT' ? `adb is not at ${file}; set ANDROID_HOME to the SDK root` : `adb could not start: ${error.message}`));
  });
  child.on('close', (status) => { clearTimeout(timer); settle({ status, stdout: Buffer.concat(out).toString('utf8'), stderr: Buffer.concat(err).toString('utf8') }); });
});

/** Dump the attached device's active window and rank its controls. */
export async function inspectAndroid(args: InspectAndroidArgs, deps: { exec?: Exec; adb?: string } = {}): Promise<InspectAndroidResult> {
  if (args.serial !== undefined && !serialPattern.test(args.serial)) throw new Error('serial is not an adb serial');
  if (args.package !== undefined && !packagePattern.test(args.package)) throw new Error('package is not an Android package name');
  const run = deps.exec ?? exec;
  const timeoutMs = Math.min(Math.max(args.timeoutMs ?? 30_000, 1_000), 120_000);
  let adb = deps.adb;
  if (!adb) {
    const sdk = androidDefaults().sdkRoot;
    if (!sdk) throw new Error('no Android SDK found; set ANDROID_HOME to the SDK root');
    adb = androidToolPaths(sdk).adb;
  }
  let serial = args.serial;
  if (!serial) {
    const listed = await run(adb, ['devices'], timeoutMs);
    const ready = listed.stdout.split('\n').map((line) => /^(\S+)\tdevice$/.exec(line.trim())?.[1]).filter((value): value is string => !!value);
    if (ready.length !== 1) throw new Error(ready.length ? `${ready.length} devices are attached; choose one with --serial (adb devices lists them)` : 'no device is attached and ready; start an emulator or connect a device');
    serial = ready[0];
  }
  // exec-out keeps the XML byte-exact; /dev/tty writes nothing to the device.
  const dumped = await run(adb, ['-s', serial, 'exec-out', 'uiautomator', 'dump', '/dev/tty'], timeoutMs);
  // uiautomator appends "UI hierchary dumped to: /dev/tty" after the document.
  const end = dumped.stdout.lastIndexOf('</hierarchy>');
  const xml = end < 0 ? dumped.stdout : dumped.stdout.slice(0, end + '</hierarchy>'.length);
  if (dumped.status !== 0 || !xml.includes('<hierarchy')) {
    const reason = `${dumped.stdout}\n${dumped.stderr}`;
    if (/could not get idle state/i.test(reason)) throw new Error('uiautomator could not get an idle state: the screen kept animating. Stop the animation (or turn off animator scale in Developer options) and retry');
    if (/null root node|UiAutomation not connected|already registered/i.test(reason)) throw new Error('uiautomator is in use by another automation session (an Appium or UiAutomator2 server); stop it and retry');
    if (/not found|offline|unauthorized/i.test(reason)) throw new Error('the device is not available to adb; check adb devices');
    throw new Error('the device returned no UI hierarchy');
  }
  return inspectAndroidHierarchy(xml, { package: args.package });
}

const badge: Record<NativeStability, string> = { stable: '🟢 stable', repeated: '🟡 repeated ID', copy: '🟠 copy only', none: '🔴 none' };

export function renderAndroidInspectReport(result: InspectAndroidResult, opts: { now?: Date } = {}): string {
  const date = (opts.now ?? new Date()).toISOString().slice(0, 10);
  const t = result.testability;
  const lines: string[] = [`# Screen Inspection — Android${result.packages.length ? ` · ${result.packages.join(', ')}` : ''}`, ''];
  for (const warning of result.warnings) lines.push(`> ⚠️ ${warning}`, '');
  const shares = [t.stable && `${t.stable} with a unique test ID`, t.repeated && `${t.repeated} with a shared ID`,
    t.copy && `${t.copy} by copy only`, t.none && `${t.none} without a handle`].filter(Boolean);
  lines.push(`**Testability: ${t.score} / 100**   ·   ${t.controls} ${t.controls === 1 ? 'control' : 'controls'}${shares.length ? `: ${shares.join(' · ')}` : ''}   ·   inspected ${date}`, '');
  lines.push(`\`${meter(t.score, 100, 24)}\` ${t.score} / 100`, '');
  if (t.note) lines.push(`_${t.note}_`, '');
  if (result.controls.length) {
    lines.push('## Controls, weakest handle first', '', '| Control | Class | Handle | 9l locator |', '|---------|-------|:------:|------------|');
    for (const control of result.controls) {
      const name = control.label || `(unlabelled, ${control.bounds})`;
      lines.push(`| ${cell(name)} | ${cell(control.className.replace(/^android\.(widget|view)\./, ''))} | ${badge[control.stability]} | ${control.locator ? codeSpan(cell(control.locator)) : '—'} |`);
    }
    if (result.truncated) lines.push('', `…and ${result.truncated} more controls with stronger handles.`);
    lines.push('');
    const notes = new Map<string, string[]>();
    for (const control of result.controls) {
      if (!control.note) continue;
      notes.set(control.note, [...(notes.get(control.note) ?? []), control.label || `(unlabelled, ${control.bounds})`]);
    }
    if (notes.size) {
      lines.push('### What to fix', '');
      for (const [note, labels] of notes) {
        lines.push(`- **${cell(labels.slice(0, 5).join(', '))}${labels.length > 5 ? ` and ${labels.length - 5} more` : ''}** — ${note}`);
      }
      lines.push('');
    }
    lines.push('A copy-only control is used through the Android driver\'s `controls` option, for example `controls: {save: {text: "Save"}}` and then `app.getByTestId("save")`.', '');
  }
  lines.push('---', '', '_Inspected by **9lives** · `9l inspect --android` · one read-only uiautomator dump of the active window · local, no account_', '');
  return lines.join('\n');
}
