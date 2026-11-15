package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json/v2"
	"fmt"
	"math"
)

// The glTF constants that the model uses.
const (
	glbMagic        = 0x46546c67 // "glTF"
	glbVersion      = 2
	chunkJSON       = 0x4e4f534a // "JSON"
	chunkBIN        = 0x004e4942 // "BIN\0"
	componentFloat  = 5126
	componentUint16 = 5123
	targetArray     = 34962
	targetIndices   = 34963
)

// materialSpec is the material of a part. The ride page uses the base color
// of each part except the shell, which has the class color. Other glTF
// viewers also use the roughness.
type materialSpec struct {
	color     [3]float64
	roughness float64
}

var materials = map[string]materialSpec{
	"shell": {[3]float64{0.86, 0.88, 0.9}, 0.4},
	"glass": {[3]float64{0.05, 0.08, 0.12}, 0.1},
	"gear":  {[3]float64{0.16, 0.17, 0.19}, 0.8},
	"cabin": {[3]float64{0.22, 0.23, 0.25}, 0.7},
	"seat":  {[3]float64{0.2, 0.27, 0.4}, 0.9},
}

type gltfDocument struct {
	Asset       gltfAsset        `json:"asset"`
	Scene       int              `json:"scene"`
	Scenes      []gltfScene      `json:"scenes"`
	Nodes       []gltfNode       `json:"nodes"`
	Meshes      []gltfMesh       `json:"meshes"`
	Materials   []gltfMaterial   `json:"materials"`
	Accessors   []gltfAccessor   `json:"accessors"`
	BufferViews []gltfBufferView `json:"bufferViews"`
	Buffers     []gltfBuffer     `json:"buffers"`
}

type gltfAsset struct {
	Version   string `json:"version"`
	Generator string `json:"generator"`
}

type gltfScene struct {
	Nodes []int `json:"nodes"`
}

type gltfNode struct {
	Name        string      `json:"name"`
	Mesh        *int        `json:"mesh,omitempty"`
	Translation *[3]float64 `json:"translation,omitempty"`
	Rotation    *[4]float64 `json:"rotation,omitempty"`
	Scale       *[3]float64 `json:"scale,omitempty"`
}

type gltfMesh struct {
	Name       string          `json:"name"`
	Primitives []gltfPrimitive `json:"primitives"`
}

type gltfPrimitive struct {
	Attributes map[string]int `json:"attributes"`
	Indices    int            `json:"indices"`
	Material   int            `json:"material"`
}

type gltfMaterial struct {
	Name string  `json:"name"`
	PBR  gltfPBR `json:"pbrMetallicRoughness"`
}

type gltfPBR struct {
	BaseColorFactor [4]float64 `json:"baseColorFactor"`
	MetallicFactor  float64    `json:"metallicFactor"`
	RoughnessFactor float64    `json:"roughnessFactor"`
}

type gltfAccessor struct {
	BufferView    int       `json:"bufferView"`
	ComponentType int       `json:"componentType"`
	Count         int       `json:"count"`
	Type          string    `json:"type"`
	Min           []float64 `json:"min,omitempty"`
	Max           []float64 `json:"max,omitempty"`
}

type gltfBufferView struct {
	Buffer     int `json:"buffer"`
	ByteOffset int `json:"byteOffset"`
	ByteLength int `json:"byteLength"`
	Target     int `json:"target"`
}

type gltfBuffer struct {
	ByteLength int `json:"byteLength"`
}

// glbWriter collects the binary buffer and the JSON document of a GLB file.
type glbWriter struct {
	doc gltfDocument
	bin bytes.Buffer
}

// view adds data as a buffer view with target, padded to 4 bytes, and
// returns its index.
func (w *glbWriter) view(data []byte, target int) int {
	w.doc.BufferViews = append(w.doc.BufferViews, gltfBufferView{ByteOffset: w.bin.Len(), ByteLength: len(data), Target: target})
	w.bin.Write(data)
	for w.bin.Len()%4 != 0 {
		w.bin.WriteByte(0)
	}
	return len(w.doc.BufferViews) - 1
}

// vectors adds points as a float VEC3 accessor. withBounds adds the min and
// max that glTF requires for positions. The bounds use the float32 values
// that the file stores.
func (w *glbWriter) vectors(points []vec3, withBounds bool) int {
	data := make([]byte, 0, len(points)*12)
	low := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	high := [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, point := range points {
		for i, value := range point.arr() {
			stored := float32(value)
			data = binary.LittleEndian.AppendUint32(data, math.Float32bits(stored))
			low[i], high[i] = min(low[i], float64(stored)), max(high[i], float64(stored))
		}
	}
	accessor := gltfAccessor{BufferView: w.view(data, targetArray), ComponentType: componentFloat, Count: len(points), Type: "VEC3"}
	if withBounds {
		accessor.Min, accessor.Max = low[:], high[:]
	}
	w.doc.Accessors = append(w.doc.Accessors, accessor)
	return len(w.doc.Accessors) - 1
}

// texcoords adds texture coordinates as a float VEC2 accessor.
func (w *glbWriter) texcoords(uvs [][2]float64) int {
	data := make([]byte, 0, len(uvs)*8)
	for _, uv := range uvs {
		for _, value := range uv {
			data = binary.LittleEndian.AppendUint32(data, math.Float32bits(float32(value)))
		}
	}
	w.doc.Accessors = append(w.doc.Accessors, gltfAccessor{BufferView: w.view(data, targetArray), ComponentType: componentFloat, Count: len(uvs), Type: "VEC2"})
	return len(w.doc.Accessors) - 1
}

// indices adds the triangle indices as an unsigned short accessor.
func (w *glbWriter) indices(values []uint16) int {
	data := make([]byte, 0, len(values)*2)
	for _, value := range values {
		data = binary.LittleEndian.AppendUint16(data, value)
	}
	w.doc.Accessors = append(w.doc.Accessors, gltfAccessor{BufferView: w.view(data, targetIndices), ComponentType: componentUint16, Count: len(values), Type: "SCALAR"})
	return len(w.doc.Accessors) - 1
}

// encodeGLB returns the model as a binary glTF file. Each part is a node
// with a mesh and a material of the same name, and each mesh has texture
// coordinates. The eye is a node with no mesh. Each screen is a node with
// no mesh, with the translation, the rotation, and the scale of the
// screen frame.
func encodeGLB(m model) ([]byte, error) {
	w := glbWriter{doc: gltfDocument{Asset: gltfAsset{Version: "2.0", Generator: "podsim podmodel"}, Scenes: []gltfScene{{}}}}
	for _, p := range m.parts {
		spec, ok := materials[p.name]
		if !ok {
			return nil, fmt.Errorf("part %s has no material", p.name)
		}
		w.doc.Materials = append(w.doc.Materials, gltfMaterial{Name: p.name, PBR: gltfPBR{
			BaseColorFactor: [4]float64{spec.color[0], spec.color[1], spec.color[2], 1}, RoughnessFactor: spec.roughness,
		}})
		primitive := gltfPrimitive{
			Attributes: map[string]int{
				"POSITION":   w.vectors(p.positions, true),
				"NORMAL":     w.vectors(p.normals, false),
				"TEXCOORD_0": w.texcoords(p.uvs),
			},
			Indices:  w.indices(p.indices),
			Material: len(w.doc.Materials) - 1,
		}
		w.doc.Meshes = append(w.doc.Meshes, gltfMesh{Name: p.name, Primitives: []gltfPrimitive{primitive}})
		mesh := len(w.doc.Meshes) - 1
		w.doc.Nodes = append(w.doc.Nodes, gltfNode{Name: p.name, Mesh: &mesh})
	}
	eye := m.eye.arr()
	w.doc.Nodes = append(w.doc.Nodes, gltfNode{Name: "eye", Translation: &eye})
	for _, s := range m.screens {
		w.doc.Nodes = append(w.doc.Nodes, gltfNode{
			Name: s.name, Translation: new(s.center.arr()), Rotation: new(s.rotation.arr()), Scale: new([3]float64{s.width, s.height, 1}),
		})
	}
	for i := range w.doc.Nodes {
		w.doc.Scenes[0].Nodes = append(w.doc.Scenes[0].Nodes, i)
	}
	w.doc.Buffers = []gltfBuffer{{ByteLength: w.bin.Len()}}
	text, err := json.Marshal(w.doc, json.Deterministic(true))
	if err != nil {
		return nil, fmt.Errorf("encode the glTF document: %w", err)
	}
	for len(text)%4 != 0 {
		text = append(text, ' ')
	}
	total := 12 + 8 + len(text) + 8 + w.bin.Len()
	if total > math.MaxUint32 {
		return nil, fmt.Errorf("the GLB file has %d bytes, more than its 32-bit length allows", total)
	}
	var out bytes.Buffer
	// #nosec G115 -- The check above keeps total, and so each chunk length, in 32 bits.
	for _, word := range []uint32{glbMagic, glbVersion, uint32(total), uint32(len(text)), chunkJSON} {
		out.Write(binary.LittleEndian.AppendUint32(nil, word))
	}
	out.Write(text)
	for _, word := range []uint32{uint32(w.bin.Len()), chunkBIN} { // #nosec G115 -- Checked with total.
		out.Write(binary.LittleEndian.AppendUint32(nil, word))
	}
	out.Write(w.bin.Bytes())
	return out.Bytes(), nil
}
