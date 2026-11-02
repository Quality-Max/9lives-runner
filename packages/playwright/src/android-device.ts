// Bounded readers of Android device state. They parse lines the driver
// already filtered on the device; nothing here keeps or returns the text.
// Every parser answers `unknown` when its markers are absent, so an
// unfamiliar Android build is reported as unknown, never guessed.

/** Keyguard and wakefulness markers from `dumpsys window` and `dumpsys power`. */
export const deviceStatePattern = 'mShowingLockscreen=|isStatusBarKeyguard=|(^| )showing=|mKeyguardShowing=|mDreamingLockscreen=|mInputRestricted=|mWakefulness=';
export type DeviceState = 'unlocked' | 'locked' | 'screen-off' | 'unknown';
export function parseDeviceState(text: string): DeviceState {
  const wakefulness = /mWakefulness=(\w+)/.exec(text)?.[1];
  if (wakefulness === 'Asleep' || wakefulness === 'Dozing') return 'screen-off';
  const markers = [...text.matchAll(/(?:^|[\s{,])(mShowingLockscreen|isStatusBarKeyguard|showing|mKeyguardShowing|mDreamingLockscreen|mInputRestricted)=(true|false)/g)];
  if (markers.length === 0) return 'unknown';
  return markers.some(([, , value]) => value === 'true') ? 'locked' : 'unlocked';
}

/** Window and focus markers from `dumpsys window windows`. */
export const windowPattern = 'Window #[0-9]+ Window\\{|mCurrentFocus=';
export type WindowState = {windows: number; focus: 'app' | 'foreign' | 'none' | 'unknown'};
/** Counts the windows titled with the app's package (its activity and its
 * dialogs; anonymous popups are not attributed) and classifies focus. */
export function parseWindows(text: string, appPackage: string): WindowState {
  const escaped = appPackage.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const windows = [...text.matchAll(new RegExp(`Window #[0-9]+ Window\\{[0-9a-f]+ u[0-9]+ ${escaped}(?:[/ }:]|$)`, 'gm'))].length;
  const focus = /mCurrentFocus=(null|Window\{[0-9a-f]+ u[0-9]+ ([^ /}]+)[^}]*\})/.exec(text);
  if (!focus) return {windows, focus: 'unknown'};
  if (focus[1] === 'null') return {windows, focus: 'none'};
  return {windows, focus: focus[2] === appPackage ? 'app' : 'foreign'};
}

/** Software keyboard visibility from `dumpsys input_method`. */
export const imePattern = 'mInputShown=';
export type ImeState = 'shown' | 'hidden' | 'unknown';
export function parseIme(text: string): ImeState {
  const value = /mInputShown=(true|false)/.exec(text)?.[1];
  return value === undefined ? 'unknown' : value === 'true' ? 'shown' : 'hidden';
}

/** Physical displays from `dumpsys display`: the DisplayDeviceInfo lines carry
 * the physical id, size, density and power state; the logical DisplayInfo
 * lines carry the current app size and density after rotation and overrides. */
export const displayPattern = 'DisplayDeviceInfo\\{|mBaseDisplayInfo=|mOverrideDisplayInfo=';
export type DisplayInfo = {id: string; width: number; height: number; density: number; on: boolean};
export function parseDisplays(text: string): DisplayInfo[] {
  const displays: DisplayInfo[] = [];
  for (const [, body] of text.matchAll(/DisplayDeviceInfo\{([^\n]*)/g)) {
    const id = /uniqueId="?local:([0-9]{1,24})"?/.exec(body)?.[1];
    const size = /(?:^|[\s,])([0-9]{2,5}) x ([0-9]{2,5})(?:[\s,]|$)/.exec(body);
    const density = /density ([0-9]{2,4})(?:[\s,]|$)/.exec(body)?.[1];
    const state = /state (ON|OFF|DOZE|UNKNOWN|VR)(?:[\s,}]|$)/.exec(body)?.[1];
    // A device's cached `mInfo=DisplayDeviceInfo` repeats its top-level
    // record; the first record per physical id stands.
    if (!id || !size || !density || displays.some(display => display.id === id)) continue;
    displays.push({id, width: Number(size[1]), height: Number(size[2]), density: Number(density), on: state === 'ON'});
  }
  return displays;
}
/** The display to target: the one powered on when exactly one is. */
export function activeDisplay(displays: DisplayInfo[]): DisplayInfo | undefined {
  const on = displays.filter(display => display.on);
  return on.length === 1 ? on[0] : displays.length === 1 ? displays[0] : undefined;
}
/** Current logical layout per physical display id: the app size (which
 * follows rotation) and the effective density (which follows overrides).
 * An override entry replaces the base entry of the same display. */
export type LogicalDisplay = {width: number; height: number; density: number};
export function parseLogicalDisplays(text: string): Map<string, LogicalDisplay> {
  const logical = new Map<string, LogicalDisplay>();
  for (const [, kind, body] of text.matchAll(/(mBaseDisplayInfo|mOverrideDisplayInfo)=DisplayInfo\{([^\n]*)/g)) {
    const id = /uniqueId "local:([0-9]{1,24})"/.exec(body)?.[1];
    // Whole comma-delimited fields only: `largest app` and `smallest app`
    // carry the nominal range, not the current size.
    const app = /(?:^|, )app ([0-9]{2,5}) x ([0-9]{2,5})(?:,|$)/.exec(body);
    const density = /(?:^|, )density ([0-9]{2,4})(?:[\s,(]|$)/.exec(body)?.[1];
    if (!id || !app || !density || (kind === 'mBaseDisplayInfo' && logical.has(id))) continue;
    logical.set(id, {width: Number(app[1]), height: Number(app[2]), density: Number(density)});
  }
  return logical;
}

/** Fold posture from `dumpsys device_state`: the committed state's name. A
 * device without the service has no posture to report and is flat. */
export const posturePattern = 'Committed state:|mCommittedState=|No device state|not found|Can.t find service';
export const postures = ['opened', 'half-opened', 'closed', 'flat', 'unknown'] as const;
export type Posture = typeof postures[number];
export function parsePosture(text: string): Posture {
  if (/Can.t find service|not found|No device state/i.test(text) || text.trim() === '') return text.trim() === '' ? 'unknown' : 'flat';
  const name = /(?:Committed state:|mCommittedState=)[^\n]*?name='?([A-Z_]+)'?/.exec(text)?.[1];
  // The documented state names, matched exactly; anything else is unknown.
  return ({OPENED: 'opened', HALF_OPENED: 'half-opened', CLOSED: 'closed'} as Record<string, Posture>)[name ?? ''] ?? 'unknown';
}
