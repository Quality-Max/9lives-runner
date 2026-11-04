// A native driver's bounded classification of the device or app state. It
// crosses the worker bridge as closed metadata; no screen content, window
// titles, hierarchy or device identifiers are part of it.
export const targetSetupType = 'application/vnd.9l.target-setup+json';
export const setupClassifications = ['blocked', 'dirty', 'mismatch', 'observer', 'unknown'] as const;
export type SetupClassification = typeof setupClassifications[number];
/** Reasons per classification; Go rejects any other pair. */
export const setupReasons: Record<SetupClassification, readonly string[]> = {
  blocked: ['device-locked', 'screen-off'],
  dirty: ['extra-window', 'foreign-focus', 'ime-showing'],
  /** The device is not in the posture or width the spec declared. */
  mismatch: ['posture', 'display-width', 'build-digest', 'build-version', 'build-signer', 'biometrics-unavailable'],
  /** The accessibility snapshot still reported an element whose window the
   * window manager no longer lists: observer staleness, not the app. */
  observer: ['window-gone-element-displayed'],
  unknown: ['device-state-unknown'],
};
export type TargetSetupRecord = {
  classification: SetupClassification;
  reason: string;
  /** `open`: found before any application operation; `recheck`: found after a failed assertion. */
  stage: 'open' | 'recheck';
  /** A user-declared, non-destructive recovery the driver took before the test ran. */
  recovery?: {method: 'back'; presses: number; restored: boolean};
};

export function validTargetSetup(value: unknown): value is TargetSetupRecord {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const r = value as Record<string, unknown>;
  const keys = Object.keys(r);
  if (keys.some(key => !['classification', 'reason', 'stage', 'recovery'].includes(key)) || keys.length < 3) return false;
  if (!setupClassifications.includes(r.classification as SetupClassification) || typeof r.reason !== 'string' ||
      !setupReasons[r.classification as SetupClassification].includes(r.reason) || !['open', 'recheck'].includes(r.stage as string)) return false;
  if (r.classification === 'observer' && r.stage !== 'recheck') return false;
  if (r.recovery === undefined) return true;
  const recovery = r.recovery as Record<string, unknown> | null;
  return !!recovery && typeof recovery === 'object' && !Array.isArray(recovery) && Object.keys(recovery).length === 3 &&
    recovery.method === 'back' && Number.isInteger(recovery.presses) && (recovery.presses as number) >= 0 && (recovery.presses as number) <= 16 &&
    typeof recovery.restored === 'boolean' && r.classification === 'dirty' && r.stage === 'open';
}

/** Thrown by a driver's open (or returned by its recheck) to classify the
 * device or app state instead of letting the failure read as an application
 * defect. The session stops with reason `setup_<classification>`. */
export class TargetSetupError extends Error {
  constructor(readonly record: TargetSetupRecord, cause?: unknown) {
    super(`9lives target setup ${record.classification}: ${record.reason}`, cause === undefined ? undefined : {cause});
    if (!validTargetSetup(record)) throw new Error('9lives target setup: invalid record');
  }
}

/** Opt-in, metadata-only description of a failed target operation: timing,
 * requested coordinates, viewport and the app's window count/focus at the
 * last mutation and after the failure. It states what was observed, never a
 * cause, and carries no screen content, hierarchy, text or identifiers. */
export const targetDiagnosticType = 'application/vnd.9l.target-diagnostic+json';
export type WindowObservation = {windows: number; focus: 'app' | 'foreign' | 'none' | 'unknown'};
export type TargetDiagnostic = {
  operation: string;
  reason: string;
  elapsedMs: number;
  x?: number; y?: number; point?: true;
  viewportWidth?: number; viewportHeight?: number;
  /** -1: not observed. */
  windowsBefore: number; windowsAfter: number;
  focusBefore?: WindowObservation['focus']; focusAfter?: WindowObservation['focus'];
};
export function validTargetDiagnostic(value: unknown): value is TargetDiagnostic {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const d = value as Record<string, unknown>;
  const pixel = (v: unknown) => Number.isInteger(v) && (v as number) >= 0 && (v as number) <= 65_535;
  const windows = (v: unknown) => Number.isInteger(v) && (v as number) >= -1 && (v as number) <= 1024;
  const focus = (v: unknown) => v === undefined || ['app', 'foreign', 'none', 'unknown'].includes(v as string);
  if (Object.keys(d).some(key => !['operation', 'reason', 'elapsedMs', 'x', 'y', 'point', 'viewportWidth', 'viewportHeight', 'windowsBefore', 'windowsAfter', 'focusBefore', 'focusAfter'].includes(key))) return false;
  return typeof d.operation === 'string' && /^[a-z-]{1,32}$/.test(d.operation) && typeof d.reason === 'string' && /^[a-z_]{1,64}$/.test(d.reason) &&
    Number.isInteger(d.elapsedMs) && (d.elapsedMs as number) >= 0 && (d.elapsedMs as number) <= 3_600_000 &&
    (d.point === undefined ? d.x === undefined && d.y === undefined : d.point === true && pixel(d.x) && pixel(d.y)) &&
    (d.viewportWidth === undefined ? d.viewportHeight === undefined : pixel(d.viewportWidth) && pixel(d.viewportHeight) && (d.viewportWidth as number) > 0 && (d.viewportHeight as number) > 0) &&
    windows(d.windowsBefore) && windows(d.windowsAfter) && focus(d.focusBefore) && focus(d.focusAfter);
}
