const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const { bootedSimulators, inspectIos, inspectIosSource, iosVersions, parseWdaSource, renderIosInspectReport } = require('../dist/inspect-ios.js');

function element(type, attributes = {}, children) {
  const merged = { type: `XCUIElementType${type}`, name: '', label: '', enabled: 'true', visible: 'true', x: '0', y: '0', width: '10', height: '10', ...attributes };
  const text = Object.entries(merged).filter(([, value]) => value !== undefined).map(([name, value]) => `${name}="${value}"`).join(' ');
  return children === undefined ? `<XCUIElementType${type} ${text}/>` : `<XCUIElementType${type} ${text}>${children.join('')}</XCUIElementType${type}>`;
}
const source = (...children) => `<?xml version="1.0" encoding="UTF-8"?><AppiumAUT>${element('Application', { name: 'Demo', label: 'Demo' }, [element('Window', {}, children)])}</AppiumAUT>`;
const byLabel = (result, label) => result.controls.find(control => control.label === label);

test('the iOS tool pins match the iOS driver', () => {
  assert.deepEqual(iosVersions, require('../../playwright/dist/ios-runtime.js').iosVersions);
});

test('an identifier differing from the label is stable; an identifier equal to it, or none, is label only', () => {
  const result = inspectIosSource(source(
    element('Button', { name: 'session_join', label: 'Beitreten' }),
    element('Button', { name: 'Absagen', label: 'Absagen' }),
    element('Switch', { name: '', label: 'Push', value: '1' }),
  ));
  assert.deepEqual(result.apps, ['Demo']);
  assert.equal(byLabel(result, 'Beitreten').stability, 'stable');
  assert.equal(byLabel(result, 'Beitreten').locator, 'app.getByTestId("session_join")');
  assert.equal(byLabel(result, 'Absagen').stability, 'copy');
  assert.match(byLabel(result, 'Absagen').note, /matches identifiers only/);
  assert.equal(byLabel(result, 'Push').locator, undefined);
  assert.deepEqual(result.testability, {
    controls: 3, stable: 1, repeated: 0, copy: 2, none: 0, score: 33,
    note: '2 of 3 controls have no unique test ID: 2 have only a label, which the 9l iOS driver cannot match.',
  });
});

test('an identifier on every list item is repeated', () => {
  const card = title => element('Cell', { name: 'session_card', label: title }, [element('Button', { name: 'session_join', label: 'Beitreten' })]);
  const result = inspectIosSource(source(card('Yoga'), card('Pilates')));
  assert.deepEqual(result.controls.map(control => control.stability), ['repeated', 'repeated', 'repeated', 'repeated']);
  assert.match(byLabel(result, 'Yoga').note, /2 elements share this identifier/);
});

test('unnamed wrappers, hidden elements and keyboard keys are not counted; an unlabelled control is', () => {
  const result = inspectIosSource(source(
    element('Cell', {}, [element('Button', { name: 'general', label: 'General' })]),
    element('Button', { name: 'hidden', label: 'Hidden', visible: 'false' }),
    element('Keyboard', {}, [element('Button', { name: 'Return', label: 'return' })]),
    element('Button', { x: '5', y: '6', width: '7', height: '8' }),
  ));
  assert.deepEqual(result.controls.map(control => [control.stability, control.label]), [['none', ''], ['stable', 'General']]);
  assert.match(renderIosInspectReport(result), /\(unlabelled, \[5,6\]\[7x8\]\)/);
  assert.match(renderIosInspectReport(result), /# Screen Inspection — iOS · Demo/);
});

test('a field is named by its label or placeholder, never by its typed value', () => {
  const result = inspectIosSource(source(
    element('SecureTextField', { name: 'password', label: '', placeholderValue: 'Passwort', value: 'hunter2' }),
    element('TextField', { name: '', label: '', value: 'typed value' }),
    element('Button', {}, [element('StaticText', { name: 'Weiter', label: 'Weiter' })]),
  ));
  assert.equal(JSON.stringify(result).includes('hunter2') || JSON.stringify(result).includes('typed value'), false);
  assert.equal(byLabel(result, 'Passwort').stability, 'stable');
  assert.equal(byLabel(result, 'Weiter').stability, 'none');
  assert.match(result.warnings.join(' '), /secure text field/);
});

test('escaped labels are decoded and a source without an app is refused', () => {
  const [, , button] = parseWdaSource(source(element('Button', { name: 'save', label: 'Speichern &amp; &quot;weiter&quot;' })));
  assert.equal(button.label, 'Speichern & "weiter"');
  assert.throws(() => parseWdaSource('<AppiumAUT/>'), /no app hierarchy/);
});

test('only booted iOS simulators with a well-formed UDID are offered', () => {
  const udid = 'AFEBAB5E-7DB9-4F3A-99AD-47A7944504B6';
  const json = JSON.stringify({ devices: {
    'com.apple.CoreSimulator.SimRuntime.iOS-18-6': [{ udid, state: 'Booted' }, { udid: 'not-a-udid', state: 'Booted' }],
    'com.apple.CoreSimulator.SimRuntime.watchOS-11-0': [{ udid: '11111111-2222-3333-4444-555555555555', state: 'Booted' }],
  } });
  assert.deepEqual(bootedSimulators(json), [udid]);
});

test('inspectIos checks the platform, the device choice and the pinned tools before starting Appium', async () => {
  const one = 'AFEBAB5E-7DB9-4F3A-99AD-47A7944504B6', two = '11111111-2222-3333-4444-555555555555';
  const listed = (...udids) => () => ({ status: 0, stdout: JSON.stringify({ devices: { 'com.apple.CoreSimulator.SimRuntime.iOS-18-6': udids.map(udid => ({ udid, state: 'Booted' })) } }) });
  const tools = fs.mkdtempSync(path.join(os.tmpdir(), '9l-ios-tools-'));
  for (const [name, version] of [['appium', iosVersions.appium], ['appium-xcuitest-driver', iosVersions.xcuitest]]) {
    fs.mkdirSync(path.join(tools, 'node_modules', name), { recursive: true });
    fs.writeFileSync(path.join(tools, 'node_modules', name, 'package.json'), JSON.stringify({ name, version }));
  }
  let read;
  const deps = { platform: 'darwin', toolsRoot: tools, readSource: async (...args) => { read = args; return source(element('Button', { name: 'ok', label: 'OK' })); } };
  try {
    await assert.rejects(inspectIos({}, { ...deps, platform: 'linux' }), /needs macOS/);
    await assert.rejects(inspectIos({ udid: 'x' }, deps), /not a simulator UDID/);
    await assert.rejects(inspectIos({ wdaProducts: 'relative' }, { ...deps, run: listed(one) }), /absolute Build\/Products/);
    await assert.rejects(inspectIos({}, { ...deps, run: listed() }), /no iOS simulator is booted/);
    await assert.rejects(inspectIos({}, { ...deps, run: listed(one, two) }), /2 simulators are booted; choose one with --udid/);
    await assert.rejects(inspectIos({ udid: two }, { ...deps, run: listed(one) }), /not booted/);
    await assert.rejects(inspectIos({}, { ...deps, run: () => ({ status: 1, stdout: '' }) }), /xcrun simctl failed/);
    fs.writeFileSync(path.join(tools, 'node_modules', 'appium', 'package.json'), JSON.stringify({ version: '2.0.0' }));
    await assert.rejects(inspectIos({}, { ...deps, run: listed(one) }), /appium 3\.8\.0 is not installed/);
    fs.writeFileSync(path.join(tools, 'node_modules', 'appium', 'package.json'), JSON.stringify({ version: iosVersions.appium }));
    const result = await inspectIos({}, { ...deps, run: listed(one) });
    assert.equal(read[0], one);
    assert.equal(read[1], tools);
    assert.equal(result.testability.stable, 1);
  } finally {
    fs.rmSync(tools, { recursive: true, force: true });
  }
});

test('a field holding a clear button is ranked; only an unnamed list row is skipped as a wrapper', () => {
  const result = inspectIosSource(source(
    element('SearchField', { placeholderValue: 'Suchen' }, [element('Button', { name: 'clear_search', label: 'Clear text' })]),
    element('Cell', {}, [element('Button', { name: 'general', label: 'General' })]),
  ));
  assert.deepEqual(result.controls.map(control => [control.stability, control.className, control.label]), [
    ['copy', 'XCUIElementTypeSearchField', 'Suchen'],
    ['stable', 'XCUIElementTypeButton', 'Clear text'],
    ['stable', 'XCUIElementTypeButton', 'General'],
  ]);
});

test('a response body is counted in bytes and cancelled past its limit', async () => {
  const { boundedBody } = require('../dist/inspect-ios.js');
  let cancelled = false, pulls = 0;
  const stream = () => new ReadableStream({
    pull(controller) { pulls++; controller.enqueue(new TextEncoder().encode('é'.repeat(4))); if (pulls > 1000) controller.close(); },
    cancel() { cancelled = true; },
  });
  // Four characters are eight bytes: past a six-byte limit on the first chunk.
  await assert.rejects(boundedBody(new Response(stream()), 6), /exceeds/);
  assert.equal(cancelled, true);
  assert.ok(pulls < 5, `read ${pulls} chunks after the limit`);
  assert.equal(await boundedBody(new Response('{"value":"é"}'), 64), '{"value":"é"}');
});
