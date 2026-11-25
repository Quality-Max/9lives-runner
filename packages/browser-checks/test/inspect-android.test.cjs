const assert = require('node:assert/strict');
const test = require('node:test');

const { inspectAndroid, inspectAndroidHierarchy, parseUiHierarchy, renderAndroidInspectReport } = require('../dist/inspect-android.js');

const pkg = 'com.example.app';
function node(attributes, children) {
  const merged = {
    text: '', 'resource-id': '', class: 'android.view.View', package: pkg, 'content-desc': '',
    checkable: 'false', clickable: 'false', 'long-clickable': 'false', password: 'false', enabled: 'true', bounds: '[0,0][10,10]',
    ...attributes,
  };
  const text = Object.entries(merged).map(([name, value]) => `${name}="${value}"`).join(' ');
  return children === undefined ? `<node ${text} />` : `<node ${text}>${children.join('')}</node>`;
}
const dump = (...nodes) => `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?><hierarchy rotation="0">${node({}, nodes)}</hierarchy>`;
const byLabel = (result, label) => result.controls.find(control => control.label === label);

test('a unique resource ID is stable, with the package prefix removed', () => {
  const result = inspectAndroidHierarchy(dump(
    node({ 'resource-id': `${pkg}:id/save`, clickable: 'true', text: 'Save' }),
    node({ 'resource-id': 'session_join', clickable: 'true', 'content-desc': 'Beitreten' }),
  ));
  assert.equal(byLabel(result, 'Save').stability, 'stable');
  assert.equal(byLabel(result, 'Save').locator, 'app.getByTestId("save")');
  assert.equal(byLabel(result, 'Beitreten').locator, 'app.getByTestId("session_join")');
  assert.deepEqual(result.testability, { controls: 2, stable: 2, repeated: 0, copy: 0, none: 0, score: 100 });
});

test('an ID that repeats per list item is reported as repeated, since getByTestId would be ambiguous', () => {
  const card = title => node({ 'resource-id': 'session_card', clickable: 'true', 'content-desc': title }, [
    node({ 'resource-id': 'session_join', clickable: 'true', 'content-desc': 'Beitreten' }),
  ]);
  const result = inspectAndroidHierarchy(dump(card('Yoga'), card('Pilates')));
  assert.equal(result.testability.repeated, 4);
  assert.equal(result.testability.score, 0);
  assert.match(byLabel(result, 'Yoga').note, /2 nodes on this screen share this ID/);
});

test('copy-only controls get the narrowest controls alias that resolves uniquely', () => {
  const result = inspectAndroidHierarchy(dump(
    node({ 'content-desc': 'Push', class: 'android.view.View' }),
    node({ 'content-desc': 'Push', class: 'android.widget.Switch', checkable: 'true', clickable: 'true' }),
    node({ text: 'Weiter', class: 'android.widget.Button', clickable: 'true' }),
    node({ hint: 'E-Mail', class: 'android.widget.EditText' }),
  ));
  assert.equal(byLabel(result, 'Push').locator, '{accessibilityId: "Push", className: "android.widget.Switch"}');
  assert.equal(byLabel(result, 'Weiter').locator, '{text: "Weiter"}');
  assert.equal(byLabel(result, 'E-Mail').locator, '{hint: "E-Mail"}');
  assert.ok(result.controls.every(control => control.stability === 'copy'));
  assert.match(result.testability.note, /3 are reachable only by their copy/);
});

test('a label on a non-interactive child reaches its control', () => {
  const result = inspectAndroidHierarchy(dump(
    node({ class: 'android.widget.FrameLayout', clickable: 'true' }, [node({ text: 'Abmelden', class: 'android.widget.TextView' })]),
  ));
  assert.equal(result.controls[0].stability, 'copy');
  assert.equal(result.controls[0].label, 'Abmelden');
  assert.equal(result.controls[0].locator, '{text: "Abmelden"}');
});

test('a control with no handle or only a shared label is reachable by coordinates only, and ranks first', () => {
  const result = inspectAndroidHierarchy(dump(
    node({ 'resource-id': 'ok', clickable: 'true', text: 'OK' }),
    node({ clickable: 'true', bounds: '[1,2][3,4]' }),
    node({ clickable: 'true', 'content-desc': 'Mehr' }),
    node({ clickable: 'true', 'content-desc': 'Mehr' }),
  ));
  assert.deepEqual(result.controls.map(control => control.stability), ['none', 'none', 'none', 'stable']);
  assert.match(result.controls[0].note, /no ID, text, description or hint/);
  assert.match(result.controls[1].note, /shared with other nodes/);
  assert.equal(result.testability.score, 25);
  assert.match(renderAndroidInspectReport(result), /\(unlabelled, \[1,2\]\[3,4\]\)/);
});

test('matching is package-scoped and another package is filtered out on request', () => {
  const xml = dump(
    node({ 'resource-id': 'save', clickable: 'true', text: 'Save' }),
    node({ 'resource-id': 'save', clickable: 'true', text: 'Allow', package: 'com.android.permissioncontroller' }),
  );
  const all = inspectAndroidHierarchy(xml);
  assert.deepEqual(all.apps, ['com.android.permissioncontroller', pkg]);
  assert.ok(all.controls.every(control => control.stability === 'stable'));
  const only = inspectAndroidHierarchy(xml, { package: pkg });
  assert.equal(only.controls.length, 1);
  assert.match(inspectAndroidHierarchy(xml, { package: 'com.other.app' }).warnings[0], /No node of com.other.app/);
});

test('escaped and multi-line labels are decoded, then flattened for display only', () => {
  const [, , label] = parseUiHierarchy(dump(node({}, [node({ 'content-desc': 'Yoga&#10;18:00 &amp; &quot;Raum 2&quot;', clickable: 'true' })])));
  assert.equal(label.description, 'Yoga\n18:00 & "Raum 2"');
  const result = inspectAndroidHierarchy(dump(node({ 'content-desc': 'Yoga&#10;18:00', clickable: 'true' })));
  assert.equal(result.controls[0].label, 'Yoga ⏎ 18:00');
  assert.equal(result.controls[0].locator, '{accessibilityId: "Yoga\\n18:00"}');
  assert.throws(() => parseUiHierarchy('ERROR: null root node returned by UiTestAutomationBridge.'), /no UI hierarchy/);
});

test('an empty screen warns instead of scoring', () => {
  const result = inspectAndroidHierarchy(dump(node({ text: 'Laden…' })));
  assert.equal(result.testability.controls, 0);
  assert.match(result.warnings[0], /No tappable, checkable or editable controls/);
});

const fakeExec = (responses) => {
  const calls = [];
  return { calls, exec: async (file, args) => { calls.push(args); return responses.shift(); } };
};

test('inspectAndroid picks the only ready device and strips the dump trailer', async () => {
  const xml = dump(node({ 'resource-id': 'save', clickable: 'true', text: 'Save' }));
  const { calls, exec } = fakeExec([
    { status: 0, stdout: 'List of devices attached\nemulator-5554\tdevice\nemulator-5556\toffline\n\n', stderr: '' },
    { status: 0, stdout: `${xml}UI hierchary dumped to: /dev/tty\n`, stderr: '' },
  ]);
  const result = await inspectAndroid({}, { exec, adb: 'adb' });
  assert.deepEqual(calls[1], ['-s', 'emulator-5554', 'exec-out', 'uiautomator', 'dump', '/dev/tty']);
  assert.equal(result.testability.stable, 1);
});

test('inspectAndroid refuses an ambiguous device choice and explains dump failures', async () => {
  const two = fakeExec([{ status: 0, stdout: 'List of devices attached\na\tdevice\nb\tdevice\n', stderr: '' }]);
  await assert.rejects(inspectAndroid({}, { exec: two.exec, adb: 'adb' }), /2 devices are attached; choose one with --serial/);
  const idle = fakeExec([{ status: 0, stdout: 'ERROR: could not get idle state.\n', stderr: '' }]);
  await assert.rejects(inspectAndroid({ serial: 'emulator-5554' }, { exec: idle.exec, adb: 'adb' }), /kept animating/);
  const busy = fakeExec([{ status: 0, stdout: 'ERROR: null root node returned by UiTestAutomationBridge.\n', stderr: '' }]);
  await assert.rejects(inspectAndroid({ serial: 'emulator-5554' }, { exec: busy.exec, adb: 'adb' }), /another automation session/);
  await assert.rejects(inspectAndroid({ serial: 'a b' }, { adb: 'adb' }), /not an adb serial/);
  await assert.rejects(inspectAndroid({ serial: 'x', package: 'not a package' }, { adb: 'adb' }), /not an Android package name/);
});

test("a field's typed text is never a label or an alias, and long labels are not quoted", () => {
  const result = inspectAndroidHierarchy(dump(
    node({ class: 'android.widget.EditText', text: 'hunter2', password: 'true', hint: 'Passwort' }),
    node({ class: 'android.widget.EditText', text: 'typed value' }),
    node({ clickable: 'true', 'content-desc': 'x'.repeat(201) }),
  ));
  assert.equal(result.controls.some(control => JSON.stringify(control).includes('hunter2') || JSON.stringify(control).includes('typed value')), false);
  assert.equal(byLabel(result, 'Passwort').locator, '{hint: "Passwort"}');
  assert.deepEqual(result.controls.map(control => control.stability), ['none', 'none', 'copy']);
  assert.match(result.warnings.join(' '), /password field/);
});

test('a row with its own caption beside a switch is ranked with the switch; an uncaptioned wrapper is not', () => {
  const result = inspectAndroidHierarchy(dump(
    node({ clickable: 'true', class: 'android.widget.LinearLayout' }, [
      node({ text: 'Push-Benachrichtigungen', class: 'android.widget.TextView' }),
      node({ 'resource-id': 'push_switch', class: 'android.widget.Switch', checkable: 'true', clickable: 'true' }),
    ]),
    node({ clickable: 'true', class: 'android.widget.FrameLayout' }, [
      node({ 'resource-id': 'save', class: 'android.widget.Button', clickable: 'true', text: 'Speichern' }),
    ]),
  ));
  assert.deepEqual(result.controls.map(control => [control.stability, control.label]),
    [['copy', 'Push-Benachrichtigungen'], ['stable', ''], ['stable', 'Speichern']]);
  assert.equal(result.testability.score, 67);
});
