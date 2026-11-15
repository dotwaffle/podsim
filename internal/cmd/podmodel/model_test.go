package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json/v2"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

var (
	updateModel   = flag.Bool("update-model", false, "rewrite web/pod.glb")
	updateStretch = flag.Bool("update-stretch", false, "rewrite testdata/stretch.json")
)

// modelPath is web/pod.glb, relative to this package.
var modelPath = filepath.Join("..", "..", "..", "web", "pod.glb")

func TestModelFile(t *testing.T) {
	want, err := encodeGLB(buildModel())
	if err != nil {
		t.Fatal(err)
	}
	if *updateModel {
		if writeErr := os.WriteFile(modelPath, want, 0o644); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	got, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatalf("read the model, or run go test -run TestModelFile -update-model: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("web/pod.glb is not the generator output; run go test -run TestModelFile -update-model")
	}
}

// decodedPart is a mesh of a GLB file, read back from the bytes.
type decodedPart struct {
	positions [][3]float64
	normals   [][3]float64
	uvs       [][2]float64
	indices   []int
}

// screenCorners returns the corners of a screen node in the model frame:
// the bottom left, the bottom right, the top right, and the top left, in
// the x-y plane of the node frame. The screen is the unit square of its
// node frame in the x-y plane, as encodeGLB writes it.
func screenCorners(translation [3]float64, rotation [4]float64, scale [3]float64) [4]vec3 {
	r := quat{rotation[0], rotation[1], rotation[2], rotation[3]}
	var out [4]vec3
	for i, c := range [4][2]float64{{-0.5, -0.5}, {0.5, -0.5}, {0.5, 0.5}, {-0.5, 0.5}} {
		out[i] = vec(translation).add(r.rotate(vec3{c[0] * scale[0], c[1] * scale[1], 0}))
	}
	return out
}

// decodedModel is a GLB file read back from the bytes: its parts by node
// name, the node names in order, the eye translation, the corners of each
// screen by node name, and the length of the pod.
type decodedModel struct {
	names   []string
	parts   map[string]decodedPart
	eye     [3]float64
	screens map[string][4]vec3
	length  float64
}

// The vehicle classes of internal/sim are 4 m and 6 m long.
var classLengths = []float64{4, 6}

// stretchCut is STRETCH_CUT of web/ride.js. A class that is longer than the
// model stretches only behind this z of the model.
const stretchCut = -0.2

// stretchZ is stretchZ of web/ride.js for the model length. It returns the
// z in a pod of scale times the model length of the point at z of the
// model, and the stretch of z there. testdata/stretch.json holds sample
// values, which the node tests compare with the page.
func stretchZ(z, scale float64) (float64, float64) {
	shift := (scale - 1) * modelLength / 2
	if z >= stretchCut {
		return z + shift, 1
	}
	grow := 2 * shift / (stretchCut + modelLength/2)
	return z + shift + (z-stretchCut)*grow, 1 + grow
}

// stretched returns m as the ride page draws it for a class of length
// meters. The checks of the sight lines do not use the normals, so they do
// not change.
func (m decodedModel) stretched(length float64) decodedModel {
	scale := length / modelLength
	z := func(v float64) float64 {
		out, _ := stretchZ(v, scale)
		return out
	}
	out := decodedModel{names: m.names, parts: make(map[string]decodedPart), eye: m.eye, screens: make(map[string][4]vec3), length: length}
	out.eye[2] = z(m.eye[2])
	for name, p := range m.parts {
		positions := make([][3]float64, len(p.positions))
		for i, position := range p.positions {
			positions[i] = [3]float64{position[0], position[1], z(position[2])}
		}
		p.positions = positions
		out.parts[name] = p
	}
	for name, corners := range m.screens {
		for i := range corners {
			corners[i].Z = z(corners[i].Z)
		}
		out.screens[name] = corners
	}
	return out
}

// decodeGLB reads data as a GLB file and checks its structure.
func decodeGLB(t *testing.T, data []byte) decodedModel {
	t.Helper()
	word := func(at int) int { return int(binary.LittleEndian.Uint32(data[at:])) }
	if len(data) < 28 || word(0) != glbMagic || word(4) != 2 || word(8) != len(data) {
		t.Fatalf("GLB header is not valid: %d bytes", len(data))
	}
	jsonLength := word(12)
	if word(16) != chunkJSON || jsonLength%4 != 0 || 20+jsonLength+8 > len(data) {
		t.Fatalf("JSON chunk is not valid: length %d", jsonLength)
	}
	binStart := 20 + jsonLength + 8
	binLength := word(20 + jsonLength)
	if word(24+jsonLength) != chunkBIN || binLength%4 != 0 || binStart+binLength != len(data) {
		t.Fatalf("BIN chunk is not valid: length %d", binLength)
	}
	bin := data[binStart:]
	var doc gltfDocument
	if err := json.Unmarshal(data[20:20+jsonLength], &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Asset.Version != "2.0" || len(doc.Buffers) != 1 || doc.Buffers[0].ByteLength != binLength {
		t.Fatalf("asset or buffer is not valid: %+v %+v", doc.Asset, doc.Buffers)
	}
	viewBytes := func(index int) []byte {
		view := doc.BufferViews[index]
		if view.ByteOffset%4 != 0 || view.ByteOffset+view.ByteLength > binLength {
			t.Fatalf("buffer view %d is outside the buffer: %+v", index, view)
		}
		return bin[view.ByteOffset : view.ByteOffset+view.ByteLength]
	}
	vectors := func(index int) [][3]float64 {
		accessor := doc.Accessors[index]
		raw := viewBytes(accessor.BufferView)
		if accessor.ComponentType != componentFloat || accessor.Type != "VEC3" || len(raw) != accessor.Count*12 {
			t.Fatalf("accessor %d is not a VEC3 float accessor: %+v", index, accessor)
		}
		out := make([][3]float64, accessor.Count)
		for i := range out {
			for c := range 3 {
				out[i][c] = float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[12*i+4*c:])))
			}
		}
		return out
	}
	model := decodedModel{parts: make(map[string]decodedPart), screens: make(map[string][4]vec3), length: modelLength}
	for _, node := range doc.Nodes {
		model.names = append(model.names, node.Name)
		if node.Mesh == nil {
			switch {
			case node.Translation == nil:
				t.Fatalf("node %s has no mesh and no translation", node.Name)
			case node.Name == "eye":
				model.eye = *node.Translation
			case node.Rotation == nil || node.Scale == nil:
				t.Fatalf("screen node %s has no rotation or no scale", node.Name)
			default:
				model.screens[node.Name] = screenCorners(*node.Translation, *node.Rotation, *node.Scale)
			}
			continue
		}
		mesh := doc.Meshes[*node.Mesh]
		if mesh.Name != node.Name || len(mesh.Primitives) != 1 {
			t.Fatalf("node %s has mesh %+v", node.Name, mesh)
		}
		primitive := mesh.Primitives[0]
		if doc.Materials[primitive.Material].Name != node.Name {
			t.Fatalf("node %s has material %s", node.Name, doc.Materials[primitive.Material].Name)
		}
		var p decodedPart
		positionAccessor := doc.Accessors[primitive.Attributes["POSITION"]]
		p.positions = vectors(primitive.Attributes["POSITION"])
		p.normals = vectors(primitive.Attributes["NORMAL"])
		if len(p.normals) != len(p.positions) {
			t.Fatalf("node %s has %d positions and %d normals", node.Name, len(p.positions), len(p.normals))
		}
		uvIndex, ok := primitive.Attributes["TEXCOORD_0"]
		if !ok {
			t.Fatalf("node %s has no texture coordinates", node.Name)
		}
		uvAccessor := doc.Accessors[uvIndex]
		uvRaw := viewBytes(uvAccessor.BufferView)
		if uvAccessor.ComponentType != componentFloat || uvAccessor.Type != "VEC2" || uvAccessor.Count != len(p.positions) || len(uvRaw) != uvAccessor.Count*8 {
			t.Fatalf("node %s has texture coordinate accessor %+v for %d positions", node.Name, uvAccessor, len(p.positions))
		}
		p.uvs = make([][2]float64, uvAccessor.Count)
		for i := range p.uvs {
			for c := range 2 {
				p.uvs[i][c] = float64(math.Float32frombits(binary.LittleEndian.Uint32(uvRaw[8*i+4*c:])))
			}
		}
		low, high := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}, [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
		for _, position := range p.positions {
			for c := range 3 {
				low[c], high[c] = min(low[c], position[c]), max(high[c], position[c])
			}
		}
		if !equalFloats(positionAccessor.Min, low[:]) || !equalFloats(positionAccessor.Max, high[:]) {
			t.Fatalf("node %s position bounds are %v %v, want %v %v", node.Name, positionAccessor.Min, positionAccessor.Max, low, high)
		}
		indexAccessor := doc.Accessors[primitive.Indices]
		raw := viewBytes(indexAccessor.BufferView)
		if indexAccessor.ComponentType != componentUint16 || indexAccessor.Type != "SCALAR" || len(raw) != indexAccessor.Count*2 || indexAccessor.Count%3 != 0 {
			t.Fatalf("node %s has index accessor %+v", node.Name, indexAccessor)
		}
		for i := range indexAccessor.Count {
			index := int(binary.LittleEndian.Uint16(raw[2*i:]))
			if index >= len(p.positions) {
				t.Fatalf("node %s index %d is %d, past %d vertices", node.Name, i, index, len(p.positions))
			}
			p.indices = append(p.indices, index)
		}
		model.parts[node.Name] = p
	}
	return model
}

func equalFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func encodedModel(t *testing.T) decodedModel {
	t.Helper()
	data, err := encodeGLB(buildModel())
	if err != nil {
		t.Fatal(err)
	}
	return decodeGLB(t, data)
}

// The ride page finds the parts and the eye by these node names.
func TestModelNodes(t *testing.T) {
	m := encodedModel(t)
	want := []string{"shell", "glass", "gear", "cabin", "seat", "eye", "mapScreen", "speedScreen", "routeScreen", "banner"}
	if !equalStrings(m.names, want) {
		t.Fatalf("nodes = %v, want %v", m.names, want)
	}
	if m.eye != eye.arr() {
		t.Fatalf("eye = %v, want %v", m.eye, eye.arr())
	}
}

// The pages tiles the textures, so a texture coordinate can pass 1, but a
// value far outside the range shows a bad projection.
func TestModelTextureCoordinates(t *testing.T) {
	m := encodedModel(t)
	for _, name := range []string{"shell", "cabin", "seat"} {
		low, high := [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
		for i, uv := range m.parts[name].uvs {
			for c, value := range uv {
				if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 8 {
					t.Fatalf("%s vertex %d has texture coordinates %v", name, i, uv)
				}
				low[c], high[c] = min(low[c], value), max(high[c], value)
			}
		}
		// A projection that gives one value to all vertices shows no texture.
		if high[0]-low[0] < 1 || high[1]-low[1] < 0.5 {
			t.Fatalf("%s texture coordinates span %v to %v", name, low, high)
		}
	}
	// The livery stripe is at the middle row of the texture, below the
	// windows and above the running gear.
	if v := shellProjection(vec3{0, beltLine, 0}, vec3{}); v[1] <= 0.5 {
		t.Fatalf("the belt line is at v = %v, below the stripe", v[1])
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The ride page places the model by the box of the procedural pod, so the
// model must fill that box and stay inside it.
func TestModelBounds(t *testing.T) {
	m := encodedModel(t)
	half := [3]float64{modelWidth / 2, modelHeight / 2, modelLength / 2}
	var reach [3]float64
	for name, p := range m.parts {
		for _, position := range p.positions {
			for c := range 3 {
				if math.Abs(position[c]) > half[c]+1e-6 {
					t.Fatalf("%s vertex %v is outside the box %v", name, position, half)
				}
				reach[c] = max(reach[c], math.Abs(position[c]))
			}
		}
	}
	for c := range 3 {
		if reach[c] < half[c]-1e-6 {
			t.Fatalf("the model reaches %v, want the box %v", reach, half)
		}
	}
}

// Each triangle must wind counterclockwise toward the side that its vertex
// normals point to, or the page culls the wrong faces.
func TestModelWinding(t *testing.T) {
	m := encodedModel(t)
	for name, p := range m.parts {
		for i := 0; i < len(p.indices); i += 3 {
			a, b, c := vec(p.positions[p.indices[i]]), vec(p.positions[p.indices[i+1]]), vec(p.positions[p.indices[i+2]])
			face := b.sub(a).cross(c.sub(a))
			if face.length() < 1e-12 {
				t.Fatalf("%s triangle %d has no area", name, i/3)
			}
			normal := vec(p.normals[p.indices[i]]).add(vec(p.normals[p.indices[i+1]])).add(vec(p.normals[p.indices[i+2]]))
			if face.dot(normal) <= 0 {
				t.Fatalf("%s triangle %d winds against its normals", name, i/3)
			}
		}
	}
}

func vec(a [3]float64) vec3 { return vec3{a[0], a[1], a[2]} }

// TestModelSightLines checks what the rider sees from the eye. Ahead and to
// the sides, the first surface is glass. Above and below, it is the cabin.
// The rider sees the same in each class.
func TestModelSightLines(t *testing.T) {
	model := encodedModel(t)
	tests := []struct {
		name      string
		direction vec3
		want      string
	}{
		{"ahead", vec3{0, 0, 1}, "glass"},
		{"ahead and down", vec3{0, -0.15, 1}, "glass"},
		{"ahead to the left", vec3{0.3, 0, 1}, "glass"},
		{"left", vec3{1, 0, 0}, "glass"},
		{"right", vec3{-1, 0, 0}, "glass"},
		{"behind", vec3{0, 0, -1}, "glass"},
		{"up", vec3{0, 1, 0}, "cabin"},
		{"down", vec3{0, -1, 0}, "cabin"},
		{"at the dashboard", vec3{0, -0.6, 1}, "cabin"},
		{"at the front pillar", vec3{0.62, 0, 1}, "cabin"},
		{"beside the front pillar", vec3{0.58, 0, 1}, "glass"},
		{"below the horizon", vec3{0, -0.2, 1}, "glass"},
		{"up through the front dome", vec3{0, 0.5, 1}, "glass"},
		{"at the middle pillar", vec3{0.74, 0, -0.5}, "cabin"},
		{"beside the middle pillar", vec3{0.74, 0, -0.42}, "glass"},
	}
	for _, length := range classLengths {
		m := model.stretched(length)
		for _, test := range tests {
			t.Run(fmt.Sprintf("%gm/%s", length, test.name), func(t *testing.T) {
				if got, _ := firstHit(m, vec(m.eye), test.direction.unit()); got != test.want {
					t.Fatalf("first surface = %q, want %q", got, test.want)
				}
			})
		}
	}
}

// firstHit returns the name of the part or the screen that the ray from
// origin in direction meets first, from either side of a triangle, and the
// distance to it.
func firstHit(m decodedModel, origin, direction vec3) (string, float64) {
	best, hit := math.Inf(1), ""
	try := func(name string, a, b, c vec3) {
		if distance, ok := rayTriangle(origin, direction, a, b, c); ok && distance < best {
			best, hit = distance, name
		}
	}
	for name, p := range m.parts {
		for i := 0; i < len(p.indices); i += 3 {
			try(name, vec(p.positions[p.indices[i]]), vec(p.positions[p.indices[i+1]]), vec(p.positions[p.indices[i+2]]))
		}
	}
	for name, q := range m.screens {
		try(name, q[0], q[1], q[2])
		try(name, q[0], q[2], q[3])
	}
	return hit, best
}

// TestModelScreens checks that the rider sees each screen whole, from the
// front, and that each screen is in the cabin, in each class. The sight
// lines of TestModelSightLines check that the screens do not hide the view
// ahead.
func TestModelScreens(t *testing.T) {
	for _, length := range classLengths {
		t.Run(fmt.Sprintf("%gm", length), func(t *testing.T) {
			checkScreens(t, encodedModel(t).stretched(length))
		})
	}
}

func checkScreens(t *testing.T, m decodedModel) {
	t.Helper()
	origin := vec(m.eye)
	half := [3]float64{modelWidth / 2, modelHeight / 2, m.length / 2}
	for name, corners := range m.screens {
		center := corners[0].add(corners[2]).scale(0.5)
		right, up := corners[1].sub(corners[0]).unit(), corners[3].sub(corners[0]).unit()
		// The face of the screen is toward -z of its frame, so it points to
		// the eye.
		if face := up.cross(right); face.dot(origin.sub(center).unit()) < 0.95 {
			t.Errorf("%s faces %v, not the eye", name, face)
		}
		// The top of the screen is up, so the page does not draw the
		// readouts upside down.
		if up.Y < 0.8 {
			t.Errorf("%s has up %v", name, up)
		}
		// The near plane of the seat camera is 0.3 m from the eye.
		if distance := center.sub(origin).length(); distance < 0.45 || distance > 1.2 {
			t.Errorf("%s is %.2f m from the eye", name, distance)
		}
		for i, corner := range corners {
			for c, value := range corner.arr() {
				if math.Abs(value) > half[c]-cabinInset {
					t.Errorf("%s corner %d %v is outside the cabin box", name, i, corner)
				}
			}
			// A point a little inside the corner is the first surface on its
			// sight line, so no part hides the screen and the screen is not
			// behind the cabin wall.
			// The point is off the diagonal, which the two triangles share.
			inside := corner.add(corners[(i+1)%4].sub(corner).scale(0.04)).add(corners[(i+3)%4].sub(corner).scale(0.06))
			ray := inside.sub(origin)
			if hit, distance := firstHit(m, origin, ray.unit()); hit != name || math.Abs(distance-ray.length()) > 1e-6 {
				t.Errorf("the sight line to %s corner %d meets %s at %.3f m, want the screen at %.3f m", name, i, hit, distance, ray.length())
			}
		}
	}
	if len(m.screens) != len(cabinScreens) {
		t.Fatalf("the model has %d screens, want %d", len(m.screens), len(cabinScreens))
	}
}

// TestModelStretchCut checks that the stretch of a longer class keeps the
// front of the cabin. The rider seat is ahead of the cut, and the bench is
// behind it, so no seat stretches. The cut is behind the middle pillar and
// ahead of the rear nose, so the shell stretches only where its sections
// do not change. The eye sees each screen corner at the same place in each
// class.
func TestModelStretchCut(t *testing.T) {
	if stretchCut >= -pillarHalfWidth || stretchCut <= -(modelLength/2-noseLength) {
		t.Fatalf("the cut %v is not in the middle body behind the middle pillar", stretchCut)
	}
	m := encodedModel(t)
	seat := m.parts["seat"]
	var ahead, behind int
	for i := 0; i < len(seat.indices); i += 3 {
		var side [2]int
		for _, index := range seat.indices[i : i+3] {
			if seat.positions[index][2] >= stretchCut {
				side[0]++
			} else {
				side[1]++
			}
		}
		if side[0] != 0 && side[1] != 0 {
			t.Fatalf("seat triangle %d crosses the cut at z = %v", i/3, stretchCut)
		}
		ahead, behind = ahead+side[0], behind+side[1]
	}
	if ahead == 0 || behind == 0 {
		t.Fatalf("the seat has %d vertices ahead of the cut and %d behind it, want the rider seat ahead and the bench behind", ahead, behind)
	}
	if eye.Z < stretchCut {
		t.Fatalf("the eye %v is behind the cut", eye)
	}
	base := classLengths[0]
	want := m.stretched(base)
	for _, length := range classLengths[1:] {
		got := m.stretched(length)
		for name, corners := range got.screens {
			for i, corner := range corners {
				d, e := corner.sub(vec(got.eye)), want.screens[name][i].sub(vec(want.eye))
				if d.sub(e).length() > 1e-9 {
					t.Errorf("%g m class: %s corner %d is at %v from the eye, want %v as in the %g m class", length, name, i, d, e, base)
				}
			}
		}
		if front := want.length/2 - want.eye[2]; math.Abs(got.length/2-got.eye[2]-front) > 1e-9 {
			t.Errorf("%g m class: the eye is %v m from the front, want %v m", length, got.length/2-got.eye[2], front)
		}
	}
}

// stretchFixture holds the stretch cut, the model length, and the output of
// stretchZ for sample points. The node tests of web/ride.js read the same
// file, so the two versions of the stretch cannot differ.
type stretchFixture struct {
	Cut         float64       `json:"cut"`
	ModelLength float64       `json:"modelLength"`
	Cases       []stretchCase `json:"cases"`
}

type stretchCase struct {
	Scale float64 `json:"scale"`
	Z     float64 `json:"z"`
	Want  float64 `json:"want"`
	Slope float64 `json:"slope"`
}

func TestStretchFixture(t *testing.T) {
	want := stretchFixture{Cut: stretchCut, ModelLength: modelLength}
	for _, length := range classLengths {
		for _, z := range []float64{-modelLength / 2, -1.17, -0.6, -0.3, stretchCut, -0.15, eye.Z, modelLength / 2} {
			got, slope := stretchZ(z, length/modelLength)
			want.Cases = append(want.Cases, stretchCase{length / modelLength, z, got, slope})
		}
	}
	// The ends of the model go to the ends of the class.
	for _, c := range want.Cases {
		if math.Abs(c.Z) == modelLength/2 && math.Abs(math.Abs(c.Want)-c.Scale*modelLength/2) > 1e-12 {
			t.Fatalf("stretchZ(%v, %v) = %v, want the end of the class", c.Z, c.Scale, c.Want)
		}
	}
	path := filepath.Join("testdata", "stretch.json")
	if *updateStretch {
		data, err := json.Marshal(want, json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the fixture, or run go test -run TestStretchFixture -update-stretch: %v", err)
	}
	var got stretchFixture
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Cut != want.Cut || got.ModelLength != want.ModelLength || !slices.Equal(got.Cases, want.Cases) {
		t.Fatalf("fixture = %+v, want %+v; run go test -run TestStretchFixture -update-stretch", got, want)
	}
}

// rayTriangle returns the distance along the ray to the triangle a, b, c,
// by the Moller-Trumbore method.
func rayTriangle(origin, direction, a, b, c vec3) (float64, bool) {
	edge1, edge2 := b.sub(a), c.sub(a)
	p := direction.cross(edge2)
	det := edge1.dot(p)
	if math.Abs(det) < 1e-12 {
		return 0, false
	}
	s := origin.sub(a)
	u := s.dot(p) / det
	if u < 0 || u > 1 {
		return 0, false
	}
	q := s.cross(edge1)
	v := direction.dot(q) / det
	if v < 0 || u+v > 1 {
		return 0, false
	}
	distance := edge2.dot(q) / det
	return distance, distance > 1e-9
}

// Each buffer view must start on a 4-byte boundary, the alignment of its
// float components.
func TestViewAlignment(t *testing.T) {
	var w glbWriter
	w.indices([]uint16{0, 1, 2})
	second := w.vectors([]vec3{{1, 2, 3}}, false)
	if offset := w.doc.BufferViews[w.doc.Accessors[second].BufferView].ByteOffset; offset != 8 {
		t.Fatalf("second view offset = %d, want 8", offset)
	}
}
