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
	indices   []int
}

// decodedModel is a GLB file read back from the bytes: its parts by node
// name, the node names in order, and the eye translation.
type decodedModel struct {
	names []string
	parts map[string]decodedPart
	eye   [3]float64
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
	model := decodedModel{parts: make(map[string]decodedPart)}
	for _, node := range doc.Nodes {
		model.names = append(model.names, node.Name)
		if node.Mesh == nil {
			if node.Translation != nil {
				model.eye = *node.Translation
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
	want := []string{"shell", "glass", "gear", "cabin", "eye"}
	if !equalStrings(m.names, want) {
		t.Fatalf("nodes = %v, want %v", m.names, want)
	}
	if m.eye != eye.arr() {
		t.Fatalf("eye = %v, want %v", m.eye, eye.arr())
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
			if got := firstHit(m, vec(m.eye), test.direction.unit()); got != test.want {
				t.Fatalf("first surface = %q, want %q", got, test.want)
			}
		})
	}
}

// firstHit returns the name of the part that the ray from origin in
// direction meets first, from either side of a triangle.
func firstHit(m decodedModel, origin, direction vec3) string {
	best, hit := math.Inf(1), ""
	for name, p := range m.parts {
		for i := 0; i < len(p.indices); i += 3 {
			a, b, c := vec(p.positions[p.indices[i]]), vec(p.positions[p.indices[i+1]]), vec(p.positions[p.indices[i+2]])
			if distance, ok := rayTriangle(origin, direction, a, b, c); ok && distance < best {
				best, hit = distance, name
			}
		}
	}
	return hit
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
