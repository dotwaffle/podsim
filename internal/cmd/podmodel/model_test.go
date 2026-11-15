package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json/v2"
	"flag"
	"math"
	"os"
	"path/filepath"
	"testing"
)

var updateModel = flag.Bool("update-model", false, "rewrite web/pod.glb")

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

// decodedScreen is a screen node of a GLB file: its translation, rotation,
// and scale.
type decodedScreen struct {
	translation [3]float64
	rotation    [4]float64
	scale       [3]float64
}

// corners returns the corners of the screen in the model frame, in order
// around the screen. The screen is the unit square of its node frame in
// the x-y plane, as encodeGLB writes it.
func (s decodedScreen) corners() [4]vec3 {
	r := quat{s.rotation[0], s.rotation[1], s.rotation[2], s.rotation[3]}
	var out [4]vec3
	for i, c := range [4][2]float64{{-0.5, -0.5}, {0.5, -0.5}, {0.5, 0.5}, {-0.5, 0.5}} {
		out[i] = vec(s.translation).add(r.rotate(vec3{c[0] * s.scale[0], c[1] * s.scale[1], 0}))
	}
	return out
}

// decodedModel is a GLB file read back from the bytes: its parts by node
// name, the node names in order, the eye translation, and the screens by
// node name.
type decodedModel struct {
	names   []string
	parts   map[string]decodedPart
	eye     [3]float64
	screens map[string]decodedScreen
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
	model := decodedModel{parts: make(map[string]decodedPart), screens: make(map[string]decodedScreen)}
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
				model.screens[node.Name] = decodedScreen{*node.Translation, *node.Rotation, *node.Scale}
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
func TestModelSightLines(t *testing.T) {
	m := encodedModel(t)
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
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, _ := firstHit(m, vec(m.eye), test.direction.unit()); got != test.want {
				t.Fatalf("first surface = %q, want %q", got, test.want)
			}
		})
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
	for name, s := range m.screens {
		q := s.corners()
		try(name, q[0], q[1], q[2])
		try(name, q[0], q[2], q[3])
	}
	return hit, best
}

// TestModelScreens checks that the rider sees each screen whole, from the
// front, and that each screen is in the cabin. The sight lines of
// TestModelSightLines check that the screens do not hide the view ahead.
func TestModelScreens(t *testing.T) {
	m := encodedModel(t)
	origin := vec(m.eye)
	half := [3]float64{modelWidth / 2, modelHeight / 2, modelLength / 2}
	for name, s := range m.screens {
		corners := s.corners()
		center := corners[0].add(corners[2]).scale(0.5)
		// The face of the screen is toward -z of its frame, so it points to
		// the eye.
		r := quat{s.rotation[0], s.rotation[1], s.rotation[2], s.rotation[3]}
		if face := r.rotate(vec3{0, 0, -1}); face.dot(origin.sub(center).unit()) < 0.95 {
			t.Errorf("%s faces %v, not the eye", name, face)
		}
		// The top of the screen is up, so the page does not draw the
		// readouts upside down.
		if up := r.rotate(vec3{0, 1, 0}); up.Y < 0.8 {
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
