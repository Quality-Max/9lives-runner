// The platform-neutral half of `9l inspect --android` and `--ios`: each
// control's handle, the testability score and the Markdown report. The
// platform modules decide which handle the 9l driver would use.
import { cell, codeSpan, meter } from './report';

/** `stable`: a test ID no other element has. `repeated`: a test ID other
 * elements share, so `getByTestId` fails as ambiguous while they are on
 * screen. `copy`: no usable ID, only its text or label. `none`: nothing but
 * coordinates reaches it. */
export type NativeStability = 'stable' | 'repeated' | 'copy' | 'none';

export type ScreenControl = {
  className: string;
  /** What a user perceives, bounded. Empty when it has none. */
  label: string;
  bounds: string;
  stability: NativeStability;
  /** The 9l handle: a getByTestId call, or an Android `controls` alias. */
  locator?: string;
  note?: string;
};

export type ScreenTestability = {
  controls: number; stable: number; repeated: number; copy: number; none: number; score: number; note?: string;
};

export const maxControls = 300;
const maxLabel = 80;

/** One line, bounded: a merged multi-line label keeps its breaks as ⏎. */
export const shortLabel = (value: string) => {
  const flat = value.replace(/\s*\n\s*/g, ' ⏎ ').replace(/\s+/g, ' ').trim();
  return flat.length > maxLabel ? `${flat.slice(0, maxLabel - 1)}…` : flat;
};

export function testabilityOf(controls: Array<{ stability: NativeStability }>, copyMeans: string): ScreenTestability {
  const tally = (value: NativeStability) => controls.filter((control) => control.stability === value).length;
  const testability: ScreenTestability = {
    controls: controls.length, stable: tally('stable'), repeated: tally('repeated'), copy: tally('copy'), none: tally('none'),
    score: controls.length === 0 ? 100 : Math.round((tally('stable') / controls.length) * 100),
  };
  const missing = controls.length - testability.stable;
  if (missing > 0) {
    testability.note = `${missing} of ${controls.length} controls have no unique test ID: ` +
      [testability.repeated && `${testability.repeated} share an ID`, testability.copy && `${testability.copy} ${copyMeans}`,
        testability.none && `${testability.none} only by coordinates`].filter(Boolean).join(', ') + '.';
  }
  return testability;
}

/** Weakest handle first, screen order within a handle, at most maxControls. */
export function rankControls<T extends { stability: NativeStability }>(controls: T[]): { controls: T[]; truncated?: number } {
  const order: Record<NativeStability, number> = { none: 0, copy: 1, repeated: 2, stable: 3 };
  const ranked = controls.map((control, index) => ({ control, index }))
    .sort((a, b) => order[a.control.stability] - order[b.control.stability] || a.index - b.index).map(({ control }) => control);
  const shown = ranked.slice(0, maxControls);
  return { controls: shown, ...(ranked.length > shown.length ? { truncated: ranked.length - shown.length } : {}) };
}

export type ScreenResult = {
  platform: 'android' | 'ios';
  /** Android packages, or the iOS application's name, on the inspected window. */
  apps: string[];
  controls: ScreenControl[];
  truncated?: number;
  testability: ScreenTestability;
  /** Every test ID on the inspected window, interactive or not, with the
   * number of elements that carry it. `9l assess --screens` reads it. */
  testIds: Record<string, number>;
  /** Set when the window held more distinct test IDs than are kept. */
  testIdsTruncated?: true;
  warnings: string[];
};

const maxTestIds = 2000;
/** Count the test IDs a window's elements carry, bounded. */
export function countTestIds(ids: Iterable<string | undefined>): { testIds: Record<string, number>; testIdsTruncated?: true } {
  const counts = new Map<string, number>();
  let truncated = false;
  for (const id of ids) {
    if (!id) continue;
    if (!counts.has(id) && counts.size >= maxTestIds) { truncated = true; continue; }
    counts.set(id, (counts.get(id) ?? 0) + 1);
  }
  const testIds = Object.fromEntries([...counts].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)));
  return { testIds, ...(truncated ? { testIdsTruncated: true as const } : {}) };
}

const copyBadge = { android: '🟠 copy only', ios: '🟠 label only' } as const;
const footer = {
  android: 'one read-only uiautomator dump of the active window',
  ios: 'one WebDriverAgent page source of the foreground app',
} as const;

export function renderScreenInspectReport(result: ScreenResult, opts: { now?: Date } = {}): string {
  const date = (opts.now ?? new Date()).toISOString().slice(0, 10);
  const t = result.testability;
  const badge: Record<NativeStability, string> = { stable: '🟢 stable', repeated: '🟡 repeated ID', copy: copyBadge[result.platform], none: '🔴 none' };
  const platform = result.platform === 'ios' ? 'iOS' : 'Android';
  const lines: string[] = [`# Screen Inspection — ${platform}${result.apps.length ? ` · ${result.apps.join(', ')}` : ''}`, ''];
  for (const warning of result.warnings) lines.push(`> ⚠️ ${warning}`, '');
  const shares = [t.stable && `${t.stable} with a unique test ID`, t.repeated && `${t.repeated} with a shared ID`,
    t.copy && `${t.copy} by ${result.platform === 'ios' ? 'label' : 'copy'} only`, t.none && `${t.none} without a handle`].filter(Boolean);
  lines.push(`**Testability: ${t.score} / 100**   ·   ${t.controls} ${t.controls === 1 ? 'control' : 'controls'}${shares.length ? `: ${shares.join(' · ')}` : ''}   ·   inspected ${date}`, '');
  lines.push(`\`${meter(t.score, 100, 24)}\` ${t.score} / 100`, '');
  if (t.note) lines.push(`_${t.note}_`, '');
  if (result.controls.length) {
    lines.push('## Controls, weakest handle first', '', '| Control | Class | Handle | 9l locator |', '|---------|-------|:------:|------------|');
    for (const control of result.controls) {
      const name = control.label || `(unlabelled, ${control.bounds})`;
      const className = control.className.replace(/^android\.(widget|view)\./, '').replace(/^XCUIElementType/, '');
      lines.push(`| ${cell(name)} | ${cell(className)} | ${badge[control.stability]} | ${control.locator ? codeSpan(cell(control.locator)) : '—'} |`);
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
    if (result.platform === 'android') {
      lines.push('A copy-only control is used through the Android driver\'s `controls` option, for example `controls: {save: {text: "Save"}}` and then `app.getByTestId("save")`.', '');
    }
  }
  lines.push('---', '', `_Inspected by **9lives** · \`9l inspect --${result.platform}\` · ${footer[result.platform]} · local, no account_`, '');
  return lines.join('\n');
}
