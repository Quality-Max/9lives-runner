// Bounded readers of Android device state. They parse lines the driver
// already filtered on the device; nothing here keeps or returns the text.
// Every parser answers `unknown` when its markers are absent, so an
// unfamiliar Android build is reported as unknown, never guessed.

/** Keyguard and wakefulness markers from `dumpsys window` and `dumpsys power`. */
export const deviceStatePattern = 'mShowingLockscreen=|isStatusBarKeyguard=|KeyguardServiceDelegate|mKeyguardShowing=|mDreamingLockscreen=|mInputRestricted=|mWakefulness=';
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
