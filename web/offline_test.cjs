'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const {spawnSync} = require('node:child_process');
const offline = require('./offline.js');
const fixture = name => offline.parseJSON(fs.readFileSync(path.join(__dirname, 'testdata/offline', name), 'utf8'), offline.REPORT_BYTES);
const plan = () => structuredClone(fixture('car-plan.json'));
const model = () => structuredClone(fixture('energy.json'));
const clone = value => structuredClone(value);

test('bounded strict JSON rejects ambiguity, nonfinite values, and inexact integers', () => {
  for (const text of ['{"id":1,"id":2}', '{"a":1,"\\u0061":2}', '1e999', '9007199254740993', '[1,]', 'true false', '{"a":}', '"bad\ntext"', '[', '\ufeff{}']) assert.throws(() => offline.parseJSON(text), text);
  assert.throws(() => offline.parseJSON(' '.repeat(11), 10), /limit/);
  assert.throws(() => offline.parseJSON('['.repeat(34) + '0' + ']'.repeat(34)), /structure/);
  assert.throws(() => offline.parseJSON('[' + '0,'.repeat(offline.MAX_NODES) + '0]'), /structure/);
  assert.equal(offline.parseJSON('{"__proto__":{"polluted":true},"a":0}').a, 0);
  assert.equal({}.polluted, undefined);
  assert.throws(() => offline.encode({mass_kg: 1e16}), /exact-number range/);
});

test('car authoring preserves explicit zeros, omitted private consent, and roundtrip input', () => {
  const value = plan();
  offline.validatePlan(value);
  assert.deepEqual(JSON.parse(offline.encode(offline.formPlan(value))), value);
  const shared = plan(); shared.itineraries[0].sharingConsent = 'shared';
  assert.equal(offline.validatePlan(shared).itineraries[0].sharingConsent, 'shared');
  for (const key of ['capacity', 'carSeats', 'partySize', 'departureSeconds', 'outwardSeconds', 'returnNotBeforeSeconds', 'activitySeconds', 'retrievalSeconds', 'homeboundSeconds']) {
    for (const bad of [undefined, null, '', -1, 0.5]) {
      const value = plan(), record = key === 'capacity' ? value.lots[0] : value.itineraries[0]; record[key] = bad;
      assert.throws(() => offline.validatePlan(value), `${key}=${bad}`);
    }
  }
  assert.throws(() => offline.formPlan({lots: [{id: 'lot', hub: 'hub', capacity: ''}], itineraries: []}), /including explicit zeros/);
  for (const change of [p => {p.itineraries[0].sharingConsent = '';}, p => {p.itineraries[0].sharingConsent = null;}, p => {p.itineraries[0].outwardRefusal = '';}, p => {p.itineraries[0].returnRefusal = 'retry';}, p => {p.itineraries[0].partySize = 9;}, p => {p.itineraries[0].carSeats = 0;}, p => {p.itineraries[0].extra = 1;}, p => {p.lots.push(clone(p.lots[0]));}, p => {p.itineraries.push(clone(p.itineraries[0]));}, p => {p.itineraries[0].destination = p.lots[0].hub;}, p => {p.itineraries[0].id = 'a'.repeat(65);}, p => {p.itineraries[0].id = '\ud800';}, p => {p.itineraries[0].activitySeconds = Number.MAX_SAFE_INTEGER + 1;}]) {
    const value = plan(); change(value); assert.throws(() => offline.validatePlan(value));
  }
});

test('energy authoring requires every coefficient and exact normalized fleet coverage', () => {
  const value = model(); offline.validateEnergy(value);
  assert.deepEqual(JSON.parse(offline.encode(offline.formEnergy(Object.entries(value.profiles).map(([className, profile]) => ({className, ...profile}))))), value);
  const profile = value.profiles.legacy;
  for (const key of Object.keys(profile)) {
    for (const bad of [undefined, null, '', NaN, Infinity]) {
      const value = model(); value.profiles.legacy[key] = bad; assert.throws(() => offline.validateEnergy(value));
    }
  }
  for (const [key, bad] of [['mass_kg', 0], ['constant_resistance_n', -1], ['quadratic_resistance_n_per_mps2', -1], ['drive_efficiency', 0], ['drive_efficiency', 1.01], ['recovery_fraction', -0.01], ['recovery_fraction', 1.01], ['auxiliary_watts', -1]]) {
    const value = model(); value.profiles.legacy[key] = bad; assert.throws(() => offline.validateEnergy(value));
  }
  assert.throws(() => offline.validateEnergy(value, {classes: ['group']}), /group/);
  assert.throws(() => offline.validateEnergy({model: 'flat-v1', profiles: {express: profile}}), /Unsupported/);
  assert.throws(() => offline.formEnergy([{className: 'legacy', ...profile}, {className: 'legacy', ...profile}]), /unique/);
  assert.throws(() => offline.formEnergy([{className: 'legacy', ...profile, mass_kg: ''}]), /explicit zeros/);
  const info = offline.projectInfo(fixture('project.json'));
  assert.ok(info.hubs.some(hub => hub.id === 'station-1'));
  assert.deepEqual(info.classes, ['legacy']);
  offline.validatePlan(plan(), info); offline.validateEnergy(value, info);
  assert.throws(() => offline.validatePlan(plan(), {hubs: [{id: 'other'}]}), /passenger/);
});

test('actual car and energy reports retain outcomes, missing clocks, assumptions, and signed net energy', () => {
  const outcomes = new Set();
  for (const name of ['car-report.json', 'car-censored-report.json', 'car-refused-report.json', 'car-stranded-report.json', 'car-empty-report.json']) {
    const report = fixture(name), parsed = offline.validateReport(report); assert.equal(parsed.kind, 'cars');
    for (const value of report.itineraries) outcomes.add(value.outcome);
    assert.deepEqual(JSON.parse(offline.encode(parsed.report)), JSON.parse(JSON.stringify(report)));
  }
  for (const outcome of ['completed', 'full-lot', 'recovered-refusal', 'stranded', 'censored']) assert.ok(outcomes.has(outcome), outcome);
  const report = fixture('energy-report.json'); assert.equal(offline.validateReport(report).kind, 'energy');
  const negative = clone(report); negative.results[0].energy.net_joules = -5;
  assert.equal(offline.validateReport(negative).report.results[0].energy.net_joules, -5);
  for (const mutate of [r => {delete r.results[0].energy.net_joules;}, r => {r.results[0].energy.net_joules = null;}, r => {r.results[0].energy.traction_joules = -1;}, r => {r.results[0].energy.window_end_tick = -1;}, r => {delete r.results[0].energy.mass_assumption;}, r => {r.schema_version = 14;}]) {
    const value = clone(report); mutate(value); assert.throws(() => offline.validateReport(value));
  }
  const car = fixture('car-report.json'); delete car.itineraries[0].held; assert.throws(() => offline.validateReport(car));
});

test('exports pass actual parkride and compare CLI decoders', {timeout: 180000}, t => {
  const result = spawnSync(process.env.PODSIM_GO || 'go', ['version'], {encoding: 'utf8'});
  if (result.error) { if (process.env.PODSIM_REQUIRE_GO) throw result.error; t.skip('Go not installed'); return; }
  assert.equal(result.status, 0, result.stderr);
  const parent = path.join(process.env.TMPDIR || os.tmpdir(), 'podsim', 'offline-tests');
  fs.mkdirSync(parent, {recursive: true});
  const dir = fs.mkdtempSync(path.join(parent, 'run-'));
  t.after(() => fs.rmSync(dir, {recursive: true, force: true}));
  fs.writeFileSync(path.join(dir, 'project.json'), offline.encode(fixture('project.json')));
  fs.writeFileSync(path.join(dir, 'car-plan.json'), offline.encode(offline.validatePlan(offline.formPlan(plan()))));
  fs.writeFileSync(path.join(dir, 'energy.json'), offline.encode(offline.validateEnergy(model())));
  for (const [command, args, reportName] of [
    ['parkride', ['-plan', path.join(dir, 'car-plan.json'), '-duration', '20m', '-queue-limit', '200'], 'car-report.json'],
    ['compare', ['-energy-file', path.join(dir, 'energy.json'), '-duration', '2s', '-request-every', '1s', '-redistribution-policies', 'off', '-format', 'json'], 'energy-report.json']
  ]) {
    const output = path.join(dir, reportName);
    const run = spawnSync(process.env.PODSIM_GO || 'go', ['run', `./cmd/${command}`, '-project', path.join(dir, 'project.json'), ...args, '-output', output], {cwd: path.join(__dirname, '..'), encoding: 'utf8', timeout: 80000, env: {...process.env, GOEXPERIMENT: 'jsonv2'}});
    assert.equal(run.status, 0, `${command}: ${run.stderr || run.error}`);
    const report = offline.parseJSON(fs.readFileSync(output, 'utf8'), offline.REPORT_BYTES);
    assert.equal(offline.validateReport(report).kind, command === 'parkride' ? 'cars' : 'energy');
  }
});

test('offline entry points are built and use local text-only rendering', () => {
  const html = fs.readFileSync(path.join(__dirname, 'offline.html'), 'utf8');
  const source = fs.readFileSync(path.join(__dirname, 'offline.js'), 'utf8');
  const build = fs.readFileSync(path.join(__dirname, '../internal/cmd/buildweb/main.go'), 'utf8');
  assert.match(build, /"offline.html", "offline.js"/);
  assert.match(fs.readFileSync(path.join(__dirname, 'editor.html'), 'utf8'), /href="offline.html"/);
  assert.match(html, /aria-live="polite"/);
  assert.doesNotMatch(source, /innerHTML|outerHTML|insertAdjacentHTML|fetch\(|WebSocket|localStorage/);
  assert.match(source, /finally \{ link.remove\(\); setTimeout\(\(\) => URL.revokeObjectURL/);
});

function offlinePage() {
  const vm = require('node:vm');
  const nodes = new Map();
  class Node {
    constructor(tag) { this.tag = tag; this.childNodes = []; this.listeners = {}; this.textContent = ''; this.dataset = {}; this._value = ''; }
    set id(value) { this._id = value; nodes.set(value, this); }
    get id() { return this._id; }
    set value(value) { this._value = value; if (value === '' && this.files) this.files = []; }
    get value() { return this._value; }
    get firstChild() { return this.childNodes[0]; }
    append(...children) { for (const child of children) { child.parent = this; this.childNodes.push(child); } }
    replaceChildren(...children) { this.childNodes = []; this.append(...children); }
    replaceWith(node) { const index = this.parent.childNodes.indexOf(this); node.parent = this.parent; this.parent.childNodes[index] = node; }
    addEventListener(name, callback) { this.listeners[name] = callback; }
    setAttribute(name, value) { this[name] = value; }
  }
  const document = {body: new Node('body'), createElement: tag => new Node(tag), getElementById(id) { if (!nodes.has(id)) { const node = new Node('div'); node.id = id; } return nodes.get(id); }};
  const context = vm.createContext({document, TextEncoder, setTimeout});
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'offline.js'), 'utf8'), context);
  function start(id, file) {
    const input = document.getElementById(id); input.files = [file]; input.value = file.name;
    return input.listeners.change();
  }
  return {get: id => document.getElementById(id), start};
}

function deferredFile(value, name) {
  let finish, reject;
  const pending = new Promise((resolve, failRead) => { finish = resolve; reject = failRead; });
  const text = typeof value === 'string' ? value : JSON.stringify(value);
  return {file: {name, size: Buffer.byteLength(text), text: () => pending}, finish: () => finish(text), fail: () => reject(new Error('File read failed.'))};
}

const readyFile = (value, name) => ({name, size: Buffer.byteLength(JSON.stringify(value)), text: async () => JSON.stringify(value)});

test('later import owns state, errors, and reset while older reads finish', async () => {
  for (const [olderValue, readError] of [[plan(), false], ['{invalid', false], [plan(), true]]) {
    const page = offlinePage(), newer = plan(); newer.itineraries[0].activitySeconds = 222;
    const older = deferredFile(olderValue, 'older.json');
    const oldRead = page.start('planImport', older.file);
    await page.start('planImport', readyFile(newer, 'newer.json'));
    const status = page.get('status').textContent;
    if (readError) older.fail(); else older.finish();
    await oldRead;
    assert.equal(page.get('itineraries-activitySeconds').value, 222);
    assert.equal(page.get('status').textContent, status);
    assert.equal(page.get('status').dataset.error, 'false');
  }
  const page = offlinePage(), first = deferredFile(plan(), 'older.json'), newer = plan(); newer.itineraries[0].activitySeconds = 333;
  const second = deferredFile(newer, 'newer-pending.json');
  const oldRead = page.start('planImport', first.file), newRead = page.start('planImport', second.file);
  first.finish(); await oldRead;
  assert.equal(page.get('planImport').files[0].name, 'newer-pending.json');
  second.finish(); await newRead;
  assert.equal(page.get('itineraries-activitySeconds').value, 333);
  assert.equal(page.get('planImport').files.length, 0);
});

test('different file inputs retain independent import generations', async () => {
  const page = offlinePage(), older = deferredFile(plan(), 'car-plan.json');
  const read = page.start('planImport', older.file);
  await page.start('energyImport', readyFile(model(), 'energy.json'));
  older.finish(); await read;
  assert.equal(page.get('itineraries-activitySeconds').value, plan().itineraries[0].activitySeconds);
  assert.equal(page.get('profiles-mass_kg').value, model().profiles.legacy.mass_kg);
});
