"use strict";

// The first-person ride page. See docs/design/first-person-ride.md.
// The pure functions at the top have node tests in ride_test.cjs. start()
// needs a browser, Babylon.js, and the ride WebAssembly module.
(function (root) {
  const POSE_FIELDS = 5;
  // The top of the guideway beam, in meters above the ground.
  const BEAM_TOP = 5;
  const BEAM_WIDTH = 1.4;
  const BEAM_DEPTH = 0.7;
  const PILLAR_SPACING = 60;
  const MAX_PILLARS = 60000;
  // A curved lane gets one sample at most every CURVE_STEP meters.
  const CURVE_STEP = 4;
  const MAX_CURVE_SAMPLES = 33;
  const LANES_PER_CHUNK = 2000;
  // The pod body: height and width, in meters. The length comes from the class.
  const POD_HEIGHT = 2.1;
  const POD_WIDTH = 1.5;
  // The eye of the seat camera, above the top of the beam.
  const EYE_HEIGHT = 1.35;
  const SEAT_YAW_SECONDS = 0.15;
  const FOG_START = 600;
  const FOG_END = 3000;
  const SKY = [0.62, 0.74, 0.86];
  const CLASS_COLORS = [[0.86, 0.88, 0.9], [0.32, 0.62, 0.9], [0.95, 0.66, 0.22], [0.72, 0.36, 0.86]];
  const MODULE_GZIP_LIMIT = 16 * 1024 * 1024;
  const MODULE_LIMIT = 64 * 1024 * 1024;

  // toScene maps a world point to the scene ground plane. World y points
  // south, and the scene z points north.
  function toScene(x, y) { return [x, -y]; }

  // yawOf returns the Babylon rotation about the y axis that turns the
  // forward axis (+z) to the world heading. The heading is in radians, from
  // world +x toward world +y (south).
  function yawOf(heading) { return Math.atan2(Math.cos(heading), -Math.sin(heading)); }

  // wrapAngle returns the angle in the range (-pi, pi].
  function wrapAngle(angle) {
    let wrapped = angle % (2 * Math.PI);
    if (wrapped <= -Math.PI) wrapped += 2 * Math.PI;
    if (wrapped > Math.PI) wrapped -= 2 * Math.PI;
    return wrapped;
  }

  // smoothAngle moves current toward target along the shorter way, with an
  // exponential time constant of tau seconds.
  function smoothAngle(current, target, seconds, tau) {
    if (!Number.isFinite(current)) return target;
    const fraction = tau > 0 ? 1 - Math.exp(-Math.max(0, seconds) / tau) : 1;
    return wrapAngle(current + wrapAngle(target - current) * fraction);
  }

  // laneSamples returns the world points along a lane: the two end points of
  // a straight lane, or samples of a quadratic Bezier curve.
  function laneSamples(from, to, control) {
    if (!control) return [from, to];
    const approx = Math.hypot(control.x - from.x, control.y - from.y) + Math.hypot(to.x - control.x, to.y - control.y);
    const count = Math.max(3, Math.min(MAX_CURVE_SAMPLES, Math.ceil(approx / CURVE_STEP) + 1));
    const points = [];
    for (let i = 0; i < count; i++) {
      const t = i / (count - 1), u = 1 - t;
      points.push({ x: u * u * from.x + 2 * u * t * control.x + t * t * to.x, y: u * u * from.y + 2 * u * t * control.y + t * t * to.y });
    }
    return points;
  }

  // beamGeometry returns the vertex data of a box beam along each lane. Each
  // face has its own vertices and normals, so the faces have flat shading.
  function beamGeometry(lanes, nodes) {
    const positions = [], normals = [], indices = [];
    const half = BEAM_WIDTH / 2, top = BEAM_TOP, bottom = BEAM_TOP - BEAM_DEPTH;
    for (const lane of lanes) {
      const from = nodes.get(lane.from), to = nodes.get(lane.to);
      if (!from || !to) continue;
      const points = laneSamples(from, to, lane.control).map((point) => toScene(point.x, point.y));
      const sides = [];
      for (let i = 0; i < points.length; i++) {
        const previous = points[Math.max(0, i - 1)], next = points[Math.min(points.length - 1, i + 1)];
        let dx = next[0] - previous[0], dz = next[1] - previous[1];
        const length = Math.hypot(dx, dz);
        if (length === 0) { dx = 1; dz = 0; } else { dx /= length; dz /= length; }
        // left is the horizontal unit vector to the left of travel.
        sides.push([-dz, dx]);
      }
      // Each face: two edges, given as [side sign, height], and a normal.
      const faces = [
        { edges: [[1, top], [-1, top]], normal: () => [0, 1, 0] },
        { edges: [[-1, top], [-1, bottom]], normal: (left) => [-left[0], 0, -left[1]] },
        { edges: [[-1, bottom], [1, bottom]], normal: () => [0, -1, 0] },
        { edges: [[1, bottom], [1, top]], normal: (left) => [left[0], 0, left[1]] },
      ];
      for (const face of faces) {
        const base = positions.length / 3;
        for (let i = 0; i < points.length; i++) {
          const left = sides[i], normal = face.normal(left);
          for (const [sign, height] of face.edges) {
            positions.push(points[i][0] + left[0] * half * sign, height, points[i][1] + left[1] * half * sign);
            normals.push(normal[0], normal[1], normal[2]);
          }
        }
        for (let i = 0; i + 1 < points.length; i++) {
          const a = base + 2 * i;
          indices.push(a, a + 1, a + 2, a + 1, a + 3, a + 2);
        }
      }
    }
    return { positions: new Float32Array(positions), normals: new Float32Array(normals), indices: new Uint32Array(indices) };
  }

  // pillarPoints returns scene points under the lanes, about one for each
  // PILLAR_SPACING meters of lane, at most MAX_PILLARS.
  function pillarPoints(lanes, nodes) {
    const result = [];
    for (const lane of lanes) {
      const from = nodes.get(lane.from), to = nodes.get(lane.to);
      if (!from || !to) continue;
      const points = laneSamples(from, to, lane.control);
      let carried = PILLAR_SPACING / 2;
      for (let i = 0; i + 1 < points.length; i++) {
        const a = points[i], b = points[i + 1];
        const length = Math.hypot(b.x - a.x, b.y - a.y);
        let at = carried;
        while (at < length) {
          if (result.length >= MAX_PILLARS) return result;
          result.push(toScene(a.x + (b.x - a.x) * at / length, a.y + (b.y - a.y) * at / length));
          at += PILLAR_SPACING;
        }
        carried = at - length;
      }
    }
    return result;
  }

  // stationSlabs returns a platform rectangle for each station with berths:
  // the scene center, the width along x, and the depth along z.
  function stationSlabs(stations, margin = 6) {
    const slabs = [];
    for (const station of stations) {
      if (!station.berths || station.berths.length === 0) continue;
      let minX = Infinity, maxX = -Infinity, minZ = Infinity, maxZ = -Infinity;
      for (const berth of station.berths) {
        const [x, z] = toScene(berth.x, berth.y);
        minX = Math.min(minX, x); maxX = Math.max(maxX, x); minZ = Math.min(minZ, z); maxZ = Math.max(maxZ, z);
      }
      slabs.push({ id: station.id, name: station.name, x: (minX + maxX) / 2, z: (minZ + maxZ) / 2, width: maxX - minX + 2 * margin, depth: maxZ - minZ + 2 * margin });
    }
    return slabs;
  }

  // sceneBounds returns the scene rectangle of the nodes.
  function sceneBounds(nodes) {
    let minX = Infinity, maxX = -Infinity, minZ = Infinity, maxZ = -Infinity;
    for (const node of nodes) {
      const [x, z] = toScene(node.position.x, node.position.y);
      minX = Math.min(minX, x); maxX = Math.max(maxX, x); minZ = Math.min(minZ, z); maxZ = Math.max(maxZ, z);
    }
    if (!Number.isFinite(minX)) return { minX: 0, maxX: 0, minZ: 0, maxZ: 0 };
    return { minX, maxX, minZ, maxZ };
  }

  // writePodMatrix writes the Babylon world matrix of a pod at scene (x, z)
  // with yaw into out at offset. Babylon matrices put the translation in
  // elements 12 to 14.
  function writePodMatrix(out, offset, x, z, yaw) {
    const c = Math.cos(yaw), s = Math.sin(yaw);
    out.set([c, 0, -s, 0, 0, 1, 0, 0, s, 0, c, 0, x, BEAM_TOP + POD_HEIGHT / 2, z, 1], offset);
  }

  // choosePod returns the requested pod ID when the list has it, else the
  // first pod with riders, else the first pod.
  function choosePod(pods, requested) {
    if (pods.some((pod) => pod.id === requested)) return requested;
    const busy = pods.find((pod) => pod.riders > 0);
    return busy ? busy.id : (pods[0] ? pods[0].id : "");
  }

  function podLabel(pod) {
    const trip = pod.riders > 0 ? ` ${pod.from} to ${pod.to}` : "";
    return `${pod.id} (${pod.class}) ${pod.activity}${trip}`;
  }

  // bounded fails the stream when it has more than limit bytes.
  function bounded(stream, limit, message) {
    let count = 0;
    return stream.pipeThrough(new TransformStream({ transform(chunk, controller) {
      count += chunk.byteLength;
      if (count > limit) throw new Error(message);
      controller.enqueue(chunk);
    } }));
  }

  async function loadModule(stopped) {
    if (typeof DecompressionStream !== "function" || typeof WebAssembly !== "object") throw new Error("This browser needs WebAssembly and native gzip decompression.");
    const go = new root.Go();
    const response = await fetch("ride.wasm.gz");
    if (!response.ok || !response.body) throw new Error("The ride module could not be downloaded.");
    const decoded = bounded(bounded(response.body, MODULE_GZIP_LIMIT, "The ride module is too large.").pipeThrough(new DecompressionStream("gzip")), MODULE_LIMIT, "The ride module is too large.");
    const { instance } = await WebAssembly.instantiateStreaming(new Response(decoded, { headers: { "Content-Type": "application/wasm" } }), go.importObject);
    go.run(instance).then(stopped, stopped);
    if (!root.podsimRide) throw new Error("The ride module did not start.");
    return root.podsimRide;
  }

  function color(B, rgb) { return new B.Color3(rgb[0], rgb[1], rgb[2]); }

  // buildWorld makes the static meshes of a topology and the pod meshes.
  function buildWorld(B, scene, topology) {
    const meshes = [];
    const nodes = new Map(topology.nodes.map((node) => [node.id, node.position]));
    const beamMaterial = new B.StandardMaterial("beam", scene);
    beamMaterial.diffuseColor = new B.Color3(0.78, 0.78, 0.76);
    beamMaterial.specularColor = new B.Color3(0.1, 0.1, 0.1);
    beamMaterial.backFaceCulling = false;
    for (let start = 0; start < topology.lanes.length; start += LANES_PER_CHUNK) {
      const geometry = beamGeometry(topology.lanes.slice(start, start + LANES_PER_CHUNK), nodes);
      if (geometry.indices.length === 0) continue;
      const mesh = new B.Mesh(`beams${start}`, scene);
      const data = new B.VertexData();
      data.positions = geometry.positions; data.normals = geometry.normals; data.indices = geometry.indices;
      data.applyToMesh(mesh);
      mesh.material = beamMaterial;
      mesh.isPickable = false;
      mesh.freezeWorldMatrix();
      meshes.push(mesh);
    }

    const pillar = B.MeshBuilder.CreateCylinder("pillar", { height: BEAM_TOP - BEAM_DEPTH, diameter: 0.6, tessellation: 8 }, scene);
    pillar.material = beamMaterial;
    pillar.isPickable = false;
    const points = pillarPoints(topology.lanes, nodes);
    const pillarMatrices = new Float32Array(points.length * 16);
    points.forEach(([x, z], i) => pillarMatrices.set([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, x, (BEAM_TOP - BEAM_DEPTH) / 2, z, 1], i * 16));
    if (points.length > 0) pillar.thinInstanceSetBuffer("matrix", pillarMatrices, 16, true);
    else pillar.setEnabled(false);
    meshes.push(pillar);

    const slabMaterial = new B.StandardMaterial("platform", scene);
    slabMaterial.diffuseColor = new B.Color3(0.55, 0.57, 0.6);
    for (const slab of stationSlabs(topology.stations)) {
      const mesh = B.MeshBuilder.CreateBox(`station-${slab.id}`, { width: slab.width, depth: slab.depth, height: 0.4 }, scene);
      mesh.position.set(slab.x, BEAM_TOP - BEAM_DEPTH - 0.25, slab.z);
      mesh.material = slabMaterial;
      mesh.isPickable = false;
      mesh.freezeWorldMatrix();
      meshes.push(mesh);
    }

    const bounds = sceneBounds(topology.nodes);
    const size = Math.max(bounds.maxX - bounds.minX, bounds.maxZ - bounds.minZ) + 2 * FOG_END;
    const ground = B.MeshBuilder.CreateGround("ground", { width: size, height: size }, scene);
    ground.position.set((bounds.minX + bounds.maxX) / 2, 0, (bounds.minZ + bounds.maxZ) / 2);
    const groundTexture = new B.DynamicTexture("groundTexture", { width: 256, height: 256 }, scene, true);
    const context = groundTexture.getContext();
    context.fillStyle = "#6f8a5a"; context.fillRect(0, 0, 256, 256);
    context.fillStyle = "#67814f"; context.fillRect(0, 0, 128, 128); context.fillRect(128, 128, 128, 128);
    groundTexture.update();
    groundTexture.wrapU = groundTexture.wrapV = B.Texture.WRAP_ADDRESSMODE;
    // One texture repeat covers 40 m, so the ground shows speed.
    groundTexture.uScale = groundTexture.vScale = size / 40;
    const groundMaterial = new B.StandardMaterial("ground", scene);
    groundMaterial.diffuseTexture = groundTexture;
    groundMaterial.specularColor = new B.Color3(0, 0, 0);
    ground.material = groundMaterial;
    ground.isPickable = false;
    ground.freezeWorldMatrix();
    meshes.push(ground);

    const pods = topology.classes.map((cls, index) => {
      const mesh = B.MeshBuilder.CreateBox(`pod-${cls.name}`, { width: POD_WIDTH, height: POD_HEIGHT, depth: cls.lengthMeters }, scene);
      const material = new B.StandardMaterial(`pod-${cls.name}`, scene);
      material.diffuseColor = color(B, CLASS_COLORS[index % CLASS_COLORS.length]);
      material.specularColor = new B.Color3(0.4, 0.4, 0.4);
      mesh.material = material;
      mesh.isPickable = false;
      // The instances move every frame and cover the whole network.
      mesh.alwaysSelectAsActiveMesh = true;
      meshes.push(mesh);
      return { mesh, length: cls.lengthMeters, matrices: new Float32Array(0), count: 0 };
    });
    return { meshes, pods };
  }

  // buildCabin makes the window frame and the dashboard of the seat. They
  // move with the camera.
  function buildCabin(B, scene, camera) {
    const material = new B.StandardMaterial("cabin", scene);
    material.diffuseColor = new B.Color3(0.12, 0.13, 0.15);
    material.specularColor = new B.Color3(0.2, 0.2, 0.2);
    const parts = [
      B.MeshBuilder.CreateBox("dashboard", { width: 1.6, height: 0.25, depth: 0.5 }, scene),
      B.MeshBuilder.CreateBox("pillarLeft", { width: 0.08, height: 1.2, depth: 0.08 }, scene),
      B.MeshBuilder.CreateBox("pillarRight", { width: 0.08, height: 1.2, depth: 0.08 }, scene),
      B.MeshBuilder.CreateBox("roof", { width: 1.6, height: 0.1, depth: 0.5 }, scene),
    ];
    const offsets = [[0, -0.55, 0.75], [-0.78, 0.0, 0.9], [0.78, 0.0, 0.9], [0, 0.6, 0.75]];
    parts.forEach((part, i) => {
      part.parent = camera;
      part.position.set(offsets[i][0], offsets[i][1], offsets[i][2]);
      part.material = material;
      part.isPickable = false;
    });
    return parts;
  }

  async function start() {
    const status = document.getElementById("status");
    const show = (text) => { status.hidden = false; status.textContent = text; };
    const B = root.BABYLON;
    if (!B) { show("Babylon.js did not load from https://cdn.babylonjs.com. The 2D viewer still works."); return; }
    if (!B.Engine.isSupported()) { show("This browser has no WebGL, so the ride page cannot draw."); return; }
    let ride;
    try { ride = await loadModule(() => show("The ride module stopped. Reload the page.")); } catch (error) { show(error.message); return; }

    const canvas = document.getElementById("scene");
    // useLargeWorldRendering draws relative to the camera, so a far network
    // does not make the cabin jitter.
    const engine = new B.Engine(canvas, true, { useLargeWorldRendering: true });
    const scene = new B.Scene(engine);
    scene.clearColor = new B.Color4(SKY[0], SKY[1], SKY[2], 1);
    scene.fogMode = B.Scene.FOGMODE_LINEAR;
    scene.fogColor = color(B, SKY);
    scene.fogStart = FOG_START;
    scene.fogEnd = FOG_END;
    scene.skipPointerMovePicking = true;
    const sun = new B.DirectionalLight("sun", new B.Vector3(-0.4, -1, -0.3), scene);
    sun.intensity = 0.8;
    const sky = new B.HemisphericLight("sky", new B.Vector3(0, 1, 0), scene);
    sky.intensity = 0.55;
    sky.groundColor = new B.Color3(0.35, 0.38, 0.32);
    const camera = new B.FreeCamera("seat", new B.Vector3(0, BEAM_TOP + EYE_HEIGHT, 0), scene);
    camera.minZ = 0.1;
    camera.maxZ = FOG_END + 200;
    camera.fov = 1.1;
    camera.rotation.x = 0.06;
    buildCabin(B, scene, camera);
    const instrumentation = new B.SceneInstrumentation(scene);
    instrumentation.captureFrameTime = true;

    const picker = document.getElementById("pod");
    const diagnostics = document.getElementById("diagnostics");
    const params = new URLSearchParams(root.location.search);
    let requested = params.get("pod") || "", podList = [], ids = [], version = -1;
    let generation = -1, world = null, poses = new Float64Array(0), yaw = NaN, lastTime = performance.now();
    let bridgeTime = 0, connected = false;

    picker.addEventListener("change", () => {
      requested = picker.value;
      const url = new URL(root.location.href);
      url.searchParams.set("pod", requested);
      root.history.replaceState(null, "", url);
      yaw = NaN;
    });
    picker.addEventListener("focus", () => refreshPods());

    function refreshPods() {
      podList = JSON.parse(ride.pods() || "[]");
      const chosen = choosePod(podList, requested);
      if (chosen !== requested) yaw = NaN;
      requested = chosen;
      picker.replaceChildren(...podList.map((pod) => new Option(podLabel(pod), pod.id, false, pod.id === requested)));
    }

    function rebuild() {
      const text = ride.topology();
      if (!text) return false;
      const topology = JSON.parse(text);
      if (world) for (const mesh of world.meshes) mesh.dispose(false, true);
      world = buildWorld(B, scene, topology);
      generation = topology.generation;
      version = -1;
      status.hidden = true;
      return true;
    }

    function updatePods(count) {
      const counts = world.pods.map(() => 0);
      for (let i = 0; i < count; i++) {
        if (ids[i] === requested) continue;
        counts[poses[i * POSE_FIELDS + 4] | 0]++;
      }
      world.pods.forEach((pod, index) => {
        // A larger buffer replaces the GPU buffer. Otherwise the frame
        // updates the same buffer.
        if (pod.matrices.length < counts[index] * 16) {
          pod.matrices = new Float32Array(Math.max(16, counts[index] * 32));
          pod.mesh.thinInstanceSetBuffer("matrix", pod.matrices, 16, false);
        }
        pod.count = 0;
      });
      let ridden = -1;
      for (let i = 0; i < count; i++) {
        if (ids[i] === requested) { ridden = i; continue; }
        const at = i * POSE_FIELDS, pod = world.pods[poses[at + 4] | 0];
        const [x, z] = toScene(poses[at], poses[at + 1]);
        writePodMatrix(pod.matrices, pod.count * 16, x, z, yawOf(poses[at + 2]));
        pod.count++;
      }
      for (const pod of world.pods) {
        pod.mesh.thinInstanceCount = pod.count;
        if (pod.count > 0) pod.mesh.thinInstanceBufferUpdated("matrix");
        pod.mesh.setEnabled(pod.count > 0);
      }
      return ridden;
    }

    function moveCamera(index, seconds) {
      if (index < 0) return;
      const at = index * POSE_FIELDS;
      const length = world.pods[poses[at + 4] | 0].length;
      const heading = poses[at + 2];
      yaw = smoothAngle(yaw, yawOf(heading), seconds, SEAT_YAW_SECONDS);
      // The eye is near the front of the pod.
      const forward = length / 2 - 0.6;
      const [x, z] = toScene(poses[at] + Math.cos(heading) * forward, poses[at + 1] + Math.sin(heading) * forward);
      camera.position.set(x, BEAM_TOP + EYE_HEIGHT, z);
      camera.rotation.y = yaw;
    }

    engine.runRenderLoop(() => {
      const now = performance.now(), seconds = (now - lastTime) / 1000;
      lastTime = now;
      if (generation < 0 && !rebuild()) { show("Waiting for the server…"); return; }
      const started = performance.now();
      let info = ride.frame(poses);
      if (info.count * POSE_FIELDS > poses.length) {
        poses = new Float64Array(info.count * POSE_FIELDS * 2);
        info = ride.frame(poses);
      }
      bridgeTime = performance.now() - started;
      connected = info.connected;
      if (info.generation !== generation) { rebuild(); return; }
      if (info.version !== version) {
        version = info.version;
        // ids has the order of the poses. The pod list has the order of the
        // latest state, which can be newer.
        ids = JSON.parse(ride.ids() || "null") || [];
        refreshPods();
      }
      moveCamera(updatePods(info.count), seconds);
      scene.render();
    });

    root.setInterval(() => {
      const draws = instrumentation.drawCallsCounter.lastSecAverage;
      diagnostics.textContent = `${engine.getFps().toFixed(0)} fps · bridge ${bridgeTime.toFixed(2)} ms · ${draws.toFixed(0)} draws · ${ids.length} pods · ${connected ? "connected" : "disconnected"}`;
    }, 500);
    root.addEventListener("resize", () => engine.resize());
  }

  const api = { POSE_FIELDS, BEAM_TOP, toScene, yawOf, wrapAngle, smoothAngle, laneSamples, beamGeometry, pillarPoints, stationSlabs, sceneBounds, writePodMatrix, choosePod, podLabel, start };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  root.PodsimRide = api;
})(typeof globalThis !== "undefined" ? globalThis : this);
