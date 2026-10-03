(function (root) {
  'use strict';
  const INPUT_BYTES = 10 * 1024 * 1024, REPORT_BYTES = 32 * 1024 * 1024;
  const MAX_NODES = 300000, MAX_DEPTH = 32, PAGE_SIZE = 25;
  const CLASSES = ['legacy', 'compact', 'group'];
  const TIMES = ['departureSeconds', 'outwardSeconds', 'returnNotBeforeSeconds', 'activitySeconds', 'retrievalSeconds', 'homeboundSeconds'];
  const LOT_FIELDS = ['id', 'hub', 'capacity'];
  const TRIP_FIELDS = ['id', 'carID', 'lot', 'destination', 'carSeats', 'partySize', 'sharingConsent', ...TIMES, 'outwardRefusal', 'returnRefusal'];
  const COEFFICIENTS = ['mass_kg', 'constant_resistance_n', 'quadratic_resistance_n_per_mps2', 'drive_efficiency', 'recovery_fraction', 'auxiliary_watts'];
  const fail = message => { throw new Error(message); };
  const bytes = text => new TextEncoder().encode(text).length;
  const object = (value, where) => { if (!value || typeof value !== 'object' || Array.isArray(value)) fail(`${where}: expected an object.`); return value; };
  const array = (value, where) => { if (!Array.isArray(value)) fail(`${where}: expected an array.`); return value; };
  const number = (value, where) => { if (typeof value !== 'number' || !Number.isFinite(value)) fail(`${where}: supply a finite number.`); return value; };
  const integer = (value, where, min = 0) => { number(value, where); if (!Number.isSafeInteger(value) || value < min) fail(`${where}: supply an exact integer of at least ${min}.`); return value; };
  const string = (value, where) => { if (typeof value !== 'string') fail(`${where}: expected text.`); return value; };
  const id = (value, where) => { string(value, where); if (!value || bytes(value) > 64 || /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(value)) fail(`${where}: supply 1 to 64 UTF-8 bytes.`); return value; };
  function members(value, fields, where, optional = []) {
    object(value, where);
    for (const key of Object.keys(value)) if (!fields.includes(key)) fail(`${where}: unknown member ${key}.`);
    for (const key of fields) if (!optional.includes(key) && (value[key] === undefined || value[key] === null)) fail(`${where}: supply ${key}, including explicit zeros.`);
  }
  // Parse bounded JSON before authoring or report rendering. JSON.parse alone accepts duplicate members.
  function parseJSON(text, limit = INPUT_BYTES) {
    if (typeof text !== 'string' || text.length > limit || bytes(text) > limit) fail('File exceeds this browser input limit.');
    let index = 0, nodes = 0;
    const space = () => { while (/[\t\r\n ]/.test(text[index] || 'x')) index++; };
    function tokenString() {
      const start = index++;
      while (index < text.length) {
        const c = text[index++];
        if (c === '"') return JSON.parse(text.slice(start, index));
        if (c === '\\') index++;
      }
      fail('Unclosed JSON string.');
    }
    function value(depth) {
      space();
      if (depth > MAX_DEPTH || ++nodes > MAX_NODES) fail('JSON exceeds this browser structure limit.');
      const c = text[index];
      if (c === '"') return tokenString();
      if (c === '{' || c === '[') {
        const isObject = c === '{', close = isObject ? '}' : ']';
        const result = isObject ? Object.create(null) : [];
        index++; space();
        if (text[index] === close) { index++; return result; }
        while (true) {
          space(); let key;
          if (isObject) {
            if (text[index] !== '"') fail('Expected a JSON member name.');
            key = tokenString(); space();
            if (Object.hasOwn(result, key)) fail(`Duplicate JSON member: ${key}.`);
            if (text[index++] !== ':') fail('Expected a JSON colon.');
          }
          const next = value(depth + 1);
          if (isObject) result[key] = next; else result.push(next);
          space(); const end = text[index++];
          if (end === close) return result;
          if (end !== ',') fail('Expected a JSON comma or closing bracket.');
        }
      }
      const match = /^(?:-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?|true|false|null)/.exec(text.slice(index));
      if (!match) fail('Invalid JSON value.');
      index += match[0].length;
      const result = JSON.parse(match[0]);
      if (typeof result === 'number') {
        number(result, 'JSON number');
        if (Number.isInteger(result) && !Number.isSafeInteger(result)) fail('Integer exceeds this browser exact-number range. Use the CLI for this file.');
      }
      return result;
    }
    const result = value(0); space();
    if (index !== text.length) fail('Unexpected text after JSON.');
    return result;
  }
  function projectInfo(project) {
    object(project, 'Project');
    integer(project.version, 'Project version', 1);
    const network = object(project.network, 'Project network');
    const hubs = array(network.Stations, 'Project stations').filter(station => {
      object(station, 'Station'); id(station.ID, 'Station ID');
      if (station.ParkingOnly !== undefined && typeof station.ParkingOnly !== 'boolean') fail('ParkingOnly must be boolean.');
      return !station.ParkingOnly;
    }).map(station => ({id: station.ID, name: typeof station.Name === 'string' ? station.Name : station.ID}));
    const classes = [...new Set(array(project.fleet, 'Project fleet').map(vehicle => {
      object(vehicle, 'Fleet vehicle');
      const value = vehicle.Class === undefined || vehicle.Class === '' ? 'legacy' : vehicle.Class;
      if (!CLASSES.includes(value)) fail(`Unsupported fleet class ${value}.`);
      return value;
    }))];
    return {hubs, classes, name: string(project.name, 'Project name')};
  }
  function validatePlan(plan, project) {
    members(plan, ['lots', 'itineraries'], 'Plan');
    const lots = new Map(), trips = new Set(), cars = new Set();
    const hubs = project && new Set(project.hubs.map(hub => hub.id));
    array(plan.lots, 'Lots').forEach(lot => {
      members(lot, LOT_FIELDS, 'Lot'); id(lot.id, 'Lot ID'); id(lot.hub, 'Lot hub'); integer(lot.capacity, 'Car capacity');
      if (lots.has(lot.id)) fail(`Duplicate lot ${lot.id}.`);
      if (hubs && !hubs.has(lot.hub)) fail(`Lot ${lot.id}: hub is not a passenger station in this project.`);
      lots.set(lot.id, lot);
    });
    array(plan.itineraries, 'Itineraries').forEach(trip => {
      members(trip, TRIP_FIELDS, 'Itinerary', ['sharingConsent']);
      for (const key of ['id', 'carID', 'lot', 'destination']) id(trip[key], key);
      if (trips.has(trip.id) || cars.has(trip.carID)) fail('Itinerary and car IDs must each be unique.');
      const lot = lots.get(trip.lot);
      if (!lot || trip.destination === lot.hub || hubs && !hubs.has(trip.destination)) fail(`${trip.id}: select a lot and a different passenger destination.`);
      integer(trip.carSeats, 'Car seats', 1); integer(trip.partySize, 'Party size', 1);
      if (trip.partySize > 8 || trip.partySize > trip.carSeats) fail(`${trip.id}: party must fit car seats and the native limit of eight.`);
      if (Object.hasOwn(trip, 'sharingConsent') && !['private', 'shared'].includes(trip.sharingConsent)) fail('Explicit consent must be private or shared.');
      if (trip.outwardRefusal !== 'drive-home' || trip.returnRefusal !== 'retain-car') fail('Select outward drive-home and return retain-car refusal policies.');
      const ticks = TIMES.map(key => BigInt(integer(trip[key], key)) * 60n);
      const arrival = ticks[0] + ticks[1];
      const activity = (arrival > 5184000n ? arrival : 5184000n) + ticks[3];
      const end = (activity > ticks[2] ? activity : ticks[2]) + ticks[4] + ticks[5];
      if (end > 9223372036854775807n) fail(`${trip.id}: simulation tick sum overflows.`);
      trips.add(trip.id); cars.add(trip.carID);
    });
    if (plan.lots.length * 512 + plan.itineraries.length * 4096 > 256 * 1024 * 1024) fail('Plan exceeds native retained-storage limit.');
    return plan;
  }
  function validateEnergy(energy, project) {
    members(energy, ['model', 'profiles'], 'Energy');
    if (energy.model !== 'flat-v1') fail('Select flat-v1.');
    object(energy.profiles, 'Profiles');
    if (!Object.keys(energy.profiles).length) fail('Add at least one authored class profile.');
    for (const [className, profile] of Object.entries(energy.profiles)) {
      if (!CLASSES.includes(className)) fail(`Unsupported class ${className}.`);
      members(profile, COEFFICIENTS, className);
      for (const key of COEFFICIENTS) number(profile[key], `${className} ${key}`);
      if (profile.mass_kg <= 0 || profile.constant_resistance_n < 0 || profile.quadratic_resistance_n_per_mps2 < 0 || profile.auxiliary_watts < 0 || profile.drive_efficiency <= 0 || profile.drive_efficiency > 1 || profile.recovery_fraction < 0 || profile.recovery_fraction > 1) fail(`${className}: coefficients are outside flat-v1 domains.`);
    }
    for (const className of project?.classes || []) if (!Object.hasOwn(energy.profiles, className)) fail(`Add a profile for project fleet class ${className}.`);
    return energy;
  }
  function validateReport(report) {
    object(report, 'Report');
    if (report.version === 1) {
      for (const key of ['projectHash', 'planHash', 'build']) string(report[key], key);
      for (const key of ['horizonTicks', 'endpointTick', 'queueLimit']) integer(report[key], key);
      if (report.endpointTick > report.horizonTicks) fail('Car endpoint exceeds horizon.');
      object(report.policies, 'Policies'); object(report.pods, 'Pod metrics');
      if (report.error !== undefined) string(report.error, 'Run error');
      const records = array(report.itineraries, 'Report itineraries'), lots = array(report.lots, 'Report lots');
      validatePlan({lots: lots.map(value => object(value, 'Lot state').lot), itineraries: records.map(value => object(value, 'Record').itinerary)});
      for (const value of lots) {
        integer(value.peak, 'Lot peak'); integer(value.occupancy, 'Lot final occupancy');
        if (value.occupancy > value.peak || value.peak > value.lot.capacity) fail('Invalid lot occupancy.');
      }
      for (const value of records) {
        string(value.stage, 'Stage'); string(value.outcome, 'Outcome');
        if (typeof value.held !== 'boolean') fail('Held slot must be boolean.');
        for (const key of ['carArrivalTick', 'returnEligibleTick', 'carReleaseTick', 'homeArrivalTick']) integer(value[key], key, -1);
        if (value.doorToDoorTicks !== undefined) integer(value.doorToDoorTicks, 'Door-to-door ticks');
        for (const key of ['outward', 'return']) {
          object(value[key], key);
          integer(value[key].requestID, 'Request ID', -1);
          integer(value[key].offeredTick, 'Offer tick', -1); integer(value[key].alightedTick, 'Alighted tick', -1);
          if (value[key].reason !== undefined) string(value[key].reason, 'Refusal reason');
        }
      }
      return {kind: 'cars', report};
    }
    if (report.schema_version === 15) {
      const results = array(report.results, 'Comparison results');
      if (!results.length) fail('Comparison report has no results.');
      for (const result of results) {
        object(result, 'Comparison result'); const energy = object(result.energy, 'Energy result');
        validateEnergy({model: energy.model, profiles: energy.profiles});
        string(energy.mass_assumption, 'Mass assumption'); object(energy.class_counts, 'Class counts');
        for (const [className, count] of Object.entries(energy.class_counts)) {
          if (!Object.hasOwn(energy.profiles, className)) fail('Class count has no authored profile.');
          integer(count, 'Class count');
        }
        integer(energy.window_start_tick, 'Energy window start'); integer(energy.window_end_tick, 'Energy window end');
        if (energy.window_end_tick < energy.window_start_tick) fail('Energy window runs backward.');
        for (const key of ['traction_joules', 'recovered_joules', 'auxiliary_joules', 'net_joules']) number(energy[key], key);
        if (energy.traction_joules < 0 || energy.recovered_joules < 0 || energy.auxiliary_joules < 0) fail('Gross, recovery, and auxiliary energy must be nonnegative.');
      }
      return {kind: 'energy', report};
    }
    fail('Unsupported report. Import a car report version 1 or an energy comparison schema 15.');
  }
  function encode(value) { const text = JSON.stringify(value, null, 2) + '\n'; if (bytes(text) > INPUT_BYTES) fail('Export exceeds the 10 MiB native input limit.'); parseJSON(text); return text; }
  function blank(fields) { return Object.fromEntries(fields.map(key => [key, ''])); }
  function authoredNumber(text, where, integral) {
    if (typeof text === 'number') return integral ? integer(text, where) : number(text, where);
    if (typeof text !== 'string' || !text.trim() || !/^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?$/.test(text)) fail(`${where}: supply a number, including explicit zeros.`);
    return integral ? integer(Number(text), where) : number(Number(text), where);
  }
  function formPlan(draft) {
    return {lots: draft.lots.map(lot => ({...lot, capacity: authoredNumber(lot.capacity, 'Capacity', true)})), itineraries: draft.itineraries.map(trip => {
      const result = {...trip};
      for (const key of ['carSeats', 'partySize', ...TIMES]) result[key] = authoredNumber(trip[key], key, true);
      return result;
    })};
  }
  function formEnergy(rows) {
    const profiles = Object.create(null);
    for (const row of rows) {
      if (Object.hasOwn(profiles, row.className)) fail('Class profiles must be unique.');
      profiles[row.className] = Object.fromEntries(COEFFICIENTS.map(key => [key, authoredNumber(row[key], key, false)]));
    }
    return {model: 'flat-v1', profiles};
  }
  const api = {parseJSON, projectInfo, validatePlan, validateEnergy, validateReport, encode, blank, formPlan, formEnergy, INPUT_BYTES, REPORT_BYTES, MAX_NODES, MAX_DEPTH};
  if (typeof module !== 'undefined') module.exports = api;
  root.PodsimOffline = api;
  if (typeof document === 'undefined') return;
  const $ = id => document.getElementById(id);
  const state = {plan: {lots: [], itineraries: []}, profiles: [], project: null, report: null, selection: {lots: 0, itineraries: 0, profiles: 0}, page: 0};
  const labels = {id: 'ID', carID: 'Car ID', hub: 'Passenger hub', capacity: 'Car capacity', lot: 'Car lot ID', destination: 'Passenger destination', carSeats: 'Car seats', partySize: 'Whole-party size (1 to 8)', sharingConsent: 'Consent for both pod legs', departureSeconds: 'Departure (seconds)', outwardSeconds: 'Outward car travel (seconds)', returnNotBeforeSeconds: 'Return not before (seconds)', activitySeconds: 'Minimum activity (seconds)', retrievalSeconds: 'Retrieval delay (seconds)', homeboundSeconds: 'Homebound car travel (seconds)', outwardRefusal: 'Outward queue refusal', returnRefusal: 'Return queue refusal', className: 'Normalized vehicle class', mass_kg: 'Fixed effective mass (kg, include assumed payload)', constant_resistance_n: 'Constant resistance (N)', quadratic_resistance_n_per_mps2: 'Quadratic resistance (N/(m/s)²)', drive_efficiency: 'Drive efficiency (greater than 0, at most 1)', recovery_fraction: 'Recovery fraction (0 to 1)', auxiliary_watts: 'Auxiliary power (W)'};
  function element(tag, text) { const node = document.createElement(tag); if (text !== undefined) node.textContent = text; return node; }
  function status(message, error = false) { $('status').textContent = message; $('status').dataset.error = String(error); }
  function action(work) { try { work(); } catch (error) { status(error.message, true); } }
  function options(select, values, chosen, placeholder = 'Select an explicit value') {
    select.append(element('option', placeholder)); select.firstChild.value = '';
    for (const value of values) { const option = element('option', value); option.value = value; select.append(option); }
    select.value = chosen ?? '';
  }
  function rows(kind) { return kind === 'profiles' ? state.profiles : state.plan[kind]; }
  function fields(kind) { return kind === 'lots' ? LOT_FIELDS : kind === 'itineraries' ? TRIP_FIELDS : ['className', ...COEFFICIENTS]; }
  function renderEditor(kind) {
    const entries = rows(kind), selected = Math.min(state.selection[kind], Math.max(0, entries.length - 1));
    state.selection[kind] = selected;
    const list = $(`${kind}List`); list.replaceChildren();
    entries.forEach((entry, index) => { const option = element('option', `${index + 1}. ${entry.id || entry.className || 'Unnamed'}`); option.value = String(index); list.append(option); });
    list.value = String(selected); list.disabled = !entries.length;
    const form = $(`${kind}Fields`); form.replaceChildren();
    $(`${kind}Remove`).disabled = !entries.length;
    if (!entries.length) { form.append(element('p', 'Add an entry or load a JSON file.')); return; }
    const entry = entries[selected];
    for (const key of fields(kind)) {
      const label = element('label', labels[key]);
      const choices = key === 'sharingConsent' ? ['private', 'shared'] : key === 'outwardRefusal' ? ['drive-home'] : key === 'returnRefusal' ? ['retain-car'] : key === 'className' ? CLASSES : null;
      const input = element(choices ? 'select' : 'input');
      input.id = `${kind}-${key}`; input.name = key; label.htmlFor = input.id;
      if (choices) options(input, choices, entry[key]);
      else {
        const numeric = ['capacity', 'carSeats', 'partySize', ...TIMES, ...COEFFICIENTS].includes(key);
        input.type = numeric ? 'number' : 'text'; input.value = entry[key] ?? '';
        if (numeric) { input.step = COEFFICIENTS.includes(key) ? 'any' : '1'; input.min = '0'; }
        if (['hub', 'destination'].includes(key)) input.setAttribute('list', 'hubs');
        if (key === 'lot') input.setAttribute('list', 'lotIDs');
      }
      input.addEventListener('input', () => { entry[key] = input.value; if (['id', 'className'].includes(key)) list.options[selected].textContent = `${selected + 1}. ${input.value || 'Unnamed'}`; if (kind === 'lots' && key === 'id') renderSuggestions(); });
      label.append(input); form.append(label);
    }
    if (kind === 'itineraries' && !Object.hasOwn(entry, 'sharingConsent')) {
      const option = element('option', 'Omitted in loaded file: private'); option.value = ''; $('itineraries-sharingConsent').firstChild.replaceWith(option); $('itineraries-sharingConsent').value = '';
    }
  }
  function renderSuggestions() {
    for (const [name, values] of [['hubs', state.project?.hubs.map(hub => hub.id) || []], ['lotIDs', state.plan.lots.map(lot => lot.id).filter(Boolean)]]) {
      $(name).replaceChildren(); for (const value of values) { const option = element('option'); option.value = value; $(name).append(option); }
    }
  }
  function download(name, text) {
    const url = URL.createObjectURL(new Blob([text], {type: 'application/json'}));
    const link = element('a'); link.href = url; link.download = name; document.body.append(link);
    try { link.click(); } finally { link.remove(); setTimeout(() => URL.revokeObjectURL(url), 1000); }
  }
  const importGenerations = new WeakMap();
  async function load(input, limit, accept) {
    const generation = {};
    importGenerations.set(input, generation);
    const current = () => importGenerations.get(input) === generation;
    const file = input.files?.[0]; if (!file) return;
    try {
      if (file.size > limit) fail('File exceeds this browser input limit.');
      const text = await file.text();
      if (!current()) return;
      const value = parseJSON(text, limit);
      accept(value, file.name);
    } catch (error) {
      if (current()) status(`${error.message} Previous data retained.`, true);
    } finally {
      if (current()) input.value = '';
    }
  }
  function renderReport(imported, target) {
    if (!imported) return;
    const report = imported.report;
    if (imported.kind === 'cars') {
      target.append(element('p', `Car report v1. Endpoint ${report.endpointTick} ticks. Horizon ${report.horizonTicks} ticks. 60 ticks = 1 second.`));
      if (report.error) target.append(element('p', `Run error: ${report.error}`));
      const provenance = element('pre', `Build: ${report.build}\nProject hash: ${report.projectHash}\nPlan hash: ${report.planHash}\nQueue limit: ${report.queueLimit}\nNative policies:\n${JSON.stringify(report.policies, null, 2)}\nNative pod metrics:\n${JSON.stringify(report.pods, null, 2)}`); target.append(provenance);
      const counts = new Map(); for (const value of report.itineraries) counts.set(value.outcome, (counts.get(value.outcome) || 0) + 1);
      target.append(element('p', `Outcomes: ${[...counts].map(([key, value]) => `${key}: ${value}`).join(', ') || 'No itineraries'}.`));
      appendPagedTable(target, 'Car lots: peak and final occupancy', report.lots, ['Lot', 'Hub', 'Capacity', 'Peak', 'Final'], value => [value.lot.id, value.lot.hub, value.lot.capacity, value.peak, value.occupancy]);
      appendPagedTable(target, 'All itinerary outcomes', report.itineraries, ['Itinerary / car', 'Stage', 'Outcome', 'Held slot', 'Outward refusal', 'Return refusal', 'Door-to-door (ticks)'], value => [value.itinerary.id + ' / ' + value.itinerary.carID, value.stage, value.outcome, value.held ? 'Yes' : 'No', value.outward.reason ?? 'None recorded', value.return.reason ?? 'None recorded', value.doorToDoorTicks ?? 'Not completed']);
      const selector = element('select'); selector.setAttribute('aria-label', 'Itinerary details');
      report.itineraries.forEach((value, index) => { const option = element('option', value.itinerary.id); option.value = String(index); selector.append(option); });
      const detail = element('pre'); const update = () => { detail.textContent = report.itineraries.length ? JSON.stringify(report.itineraries[Number(selector.value)], null, 2) : 'No itineraries.'; };
      selector.addEventListener('change', update); target.append(selector, detail); update();
      target.append(element('p', 'Each itinerary includes authored durations, seats, consent, policies, request IDs, and event ticks. Tick -1 means unrecorded.'));
    } else {
      target.append(element('p', `Energy comparison schema 15. ${report.results.length} results. Estimates use authored flat-v1 profiles.`));
      const selector = element('select'); selector.setAttribute('aria-label', 'Energy comparison result');
      report.results.forEach((value, index) => { const option = element('option', `${index + 1}. ${value.pattern ?? 'Pattern unavailable'} / ${value.policy ?? 'Policy unavailable'} / seed ${value.seed ?? 'unavailable'}`); option.value = String(index); selector.append(option); });
      const detail = element('div');
      function update() {
        detail.replaceChildren(); const result = report.results[Number(selector.value)], energy = result.energy;
        const totals = element('dl');
        for (const [label, key] of [['Gross traction draw (J)', 'traction_joules'], ['Recovered energy (J)', 'recovered_joules'], ['Auxiliary energy (J)', 'auxiliary_joules'], ['Signed net energy (J)', 'net_joules']]) totals.append(element('dt', label), element('dd', String(energy[key])));
        detail.append(totals, element('p', `Window: ${energy.window_start_tick} to ${energy.window_end_tick} ticks. 60 ticks = 1 second. ${energy.mass_assumption}.`));
        detail.append(element('pre', `Class counts:\n${JSON.stringify(energy.class_counts, null, 2)}\nAuthored profiles (kg, N, N/(m/s)², fractions, W):\n${JSON.stringify(energy.profiles, null, 2)}\nFull comparison result:\n${JSON.stringify(result, null, 2)}`));
        detail.append(element('p', 'This report does not supply build or project hashes. Preserve the CLI inputs and command with the report.'));
      }
      selector.addEventListener('change', update); target.append(selector, detail); update();
    }
    const details = element('details'); details.append(element('summary', 'Full imported report JSON'), element('pre', JSON.stringify(report, null, 2))); target.append(details);
  }
  function appendPagedTable(target, captionText, values, headers, valuesFor) {
    const section = element('section'), table = element('table'); table.append(element('caption', captionText));
    const header = element('tr'); for (const text of headers) { const cell = element('th', text); cell.scope = 'col'; header.append(cell); } table.append(header);
    const body = element('tbody'), controls = element('div'); let page = 0;
    const previous = element('button', 'Previous'), next = element('button', 'Next'), label = element('span');
    for (const button of [previous, next]) { button.type = 'button'; button.setAttribute('aria-label', `${button.textContent} page of ${captionText}`); }
    function show() {
      body.replaceChildren();
      for (const value of values.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE)) { const row = element('tr'); for (const text of valuesFor(value)) row.append(element('td', String(text))); body.append(row); }
      previous.disabled = page === 0; next.disabled = (page + 1) * PAGE_SIZE >= values.length;
      label.textContent = ` ${values.length} entries. Page ${page + 1} of ${Math.max(1, Math.ceil(values.length / PAGE_SIZE))}. `;
    }
    previous.addEventListener('click', () => { page--; show(); }); next.addEventListener('click', () => { page++; show(); });
    table.append(body); controls.append(previous, label, next); section.append(table, controls); target.append(section); show();
  }
  for (const kind of ['lots', 'itineraries', 'profiles']) {
    $(`${kind}Add`).addEventListener('click', () => { rows(kind).push(blank(fields(kind))); state.selection[kind] = rows(kind).length - 1; renderEditor(kind); status('New entry requires explicit values. Export files to keep your work.'); });
    $(`${kind}Remove`).addEventListener('click', () => { rows(kind).splice(state.selection[kind], 1); renderEditor(kind); renderSuggestions(); });
    $(`${kind}List`).addEventListener('change', event => { state.selection[kind] = Number(event.target.value); renderEditor(kind); }); renderEditor(kind);
  }
  $('projectImport').addEventListener('change', () => load($('projectImport'), INPUT_BYTES, (value, name) => { const info = projectInfo(value); state.project = info; $('projectInfo').textContent = `${name}: ${info.name}. Passenger hubs: ${info.hubs.map(hub => hub.id).join(', ')}. Fleet classes: ${info.classes.join(', ')}. Association only. CLI validates the project.`; renderSuggestions(); status('Project associated locally. Existing authored values retained.'); }));
  $('projectClear').addEventListener('click', () => { state.project = null; $('projectInfo').textContent = 'No project associated. Enter passenger station IDs from your project.'; renderSuggestions(); });
  $('planImport').addEventListener('change', () => load($('planImport'), INPUT_BYTES, value => { validatePlan(value, state.project); state.plan = value; state.selection.lots = state.selection.itineraries = 0; renderEditor('lots'); renderEditor('itineraries'); renderSuggestions(); status('Car plan loaded. Omitted consent stays omitted and means private.'); }));
  $('energyImport').addEventListener('change', () => load($('energyImport'), INPUT_BYTES, value => { validateEnergy(value, state.project); state.profiles = Object.entries(value.profiles).map(([className, profile]) => ({className, ...profile})); state.selection.profiles = 0; renderEditor('profiles'); status('Authored energy profiles loaded.'); }));
  $('planExport').addEventListener('click', () => action(() => { const plan = formPlan(state.plan); validatePlan(plan, state.project); download('car-plan.json', encode(plan)); status('Downloaded car-plan.json. Run parkride locally, then import car-report.json.'); }));
  $('energyExport').addEventListener('click', () => action(() => { const energy = formEnergy(state.profiles); validateEnergy(energy, state.project); download('energy.json', encode(energy)); status('Downloaded energy.json. Run compare locally, then import energy-report.json.'); }));
  $('reportImport').addEventListener('change', () => load($('reportImport'), REPORT_BYTES, value => { const imported = validateReport(value); const view = element('div'); renderReport(imported, view); $('reportView').replaceChildren(...view.childNodes); state.report = imported; status('Report loaded. All imported outcomes and authored assumptions remain available.'); }));
})(typeof globalThis === 'undefined' ? this : globalThis);
