// A native driver's bounded classification of the device or app state. It
// crosses the worker bridge as closed metadata; no screen content, window
// titles, hierarchy or device identifiers are part of it.
export const targetSetupType = 'application/vnd.9l.target-setup+json';
export const setupClassifications = ['blocked', 'dirty', 'mismatch', 'unknown'] as const;
export type SetupClassification = typeof setupClassifications[number];
/** Reasons per classification; Go rejects any other pair. */
export const setupReasons: Record<SetupClassification, readonly string[]> = {
  blocked: ['device-locked', 'screen-off'],
  dirty: ['extra-window', 'foreign-focus', 'ime-showing'],
  mismatch: [],
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
