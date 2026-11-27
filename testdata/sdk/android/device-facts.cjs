'use strict';
// Qualification-only facts the Android smoke reads from its owned emulator
// with adb, outside the accessibility reads the driver uses. Fixed words,
// counts and lengths only; device output is matched here and never kept.
const {execFile} = require('node:child_process');

// The synthetic fixture app appends one fixed-format line to its private
// files/handled when its activity is created, when it draws its content, when
// its window gains or loses focus, when a touch reaches its window (named by
// the watched control it landed on, or `other`) and when a watched click
// handler runs (testdata/android/MainActivity.java). reset() deletes the file
// with the app's other data, so the log covers the time since the last reset.
// It tells a failed text assertion apart: a tap that never reached the app,
// one that landed elsewhere, one whose handler did not run, a handler that
// ran with other field contents, or a read that did not see what the app
// drew. Only lines in the fixture's grammar are kept; anything else is counted.
const grammar = /^(create|drawn|drawn in|drawn out|focus in|focus out|touch (down|up|cancel) (recipient|amount|send|email|validate|login|sign-in|other)|click send \d{1,4} \d{1,4}|click validate (invalid|valid) \d{1,4}|click login \d{1,4}|click sign-in)$/;
const maxEvents = 40;

/** The matching lines in order, at most 40, and how many did not match. */
function parseAppLog(text) {
  const events = [];
  let other = 0, truncated = false;
  for (const line of String(text).split('\n')) {
    if (!line) continue;
    if (!grammar.test(line)) { other++; continue; }
    if (events.length === maxEvents) { truncated = true; continue; }
    events.push(line);
  }
  return {log: 'read', events, ...(other ? {other} : {}), ...(truncated ? {truncated} : {})};
}

/** How a read that did not answer ended: an answer over the bound, the file
 * did not exist, the time ran out, or the device or package could not be
 * read (after an operation timeout the driver has already stopped the
 * emulator). */
function classify(error, stderr) {
  if (error.code === 'ERR_CHILD_PROCESS_STDIO_MAXBUFFER') return 'oversized';
  if (error.killed) return 'timeout';
  if (/No such file/.test(String(stderr))) return 'missing';
  return 'unreachable';
}

/** Read the app's log; never rejects, since a missing log is a fact. */
function readAppLog(adb, serial, appPackage, timeoutMs = 5000) {
  return new Promise(resolve => {
    execFile(adb, ['-s', serial, 'shell', 'run-as', appPackage, 'cat', 'files/handled'],
      {timeout: timeoutMs, maxBuffer: 8192, encoding: 'utf8'},
      (error, stdout, stderr) => resolve(error ? {log: classify(error, stderr)} : parseAppLog(stdout)));
  });
}

/** Which window had input focus: the app's own, another one (a system
 * dialog, the launcher) or unknown. Never the window's name. */
function frontWindow(adb, serial, appPackage, timeoutMs = 5000) {
  return new Promise(resolve => {
    execFile(adb, ['-s', serial, 'shell', 'dumpsys window | grep -m1 mCurrentFocus='],
      {timeout: timeoutMs, maxBuffer: 4096, encoding: 'utf8'},
      (error, stdout) => {
        const owner = error ? undefined : /mCurrentFocus=Window\{\S+ \S+ ([^\s/}]+)/.exec(stdout)?.[1];
        resolve(owner === undefined ? 'unknown' : owner === appPackage ? 'app' : 'other');
      });
  });
}

module.exports = {parseAppLog, readAppLog, frontWindow};
