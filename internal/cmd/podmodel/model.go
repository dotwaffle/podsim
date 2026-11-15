package main

import (
	"math"
	"slices"
)

// The pod model is 4 m long, 1.5 m wide, and 2.1 m tall, with its origin at
// the center of that box. +Y is up and +Z is the front, as glTF requires.
// The ride page scales the length to each vehicle class.
const (
	modelLength = 4.0
	modelWidth  = 1.5
	modelHeight = 2.1
	// The body is a loft of rounded sections above the running gear.
	// bodyCenter is the height of the section center, and bodyHalfWidth and
	// bodyHalfHeight are its half axes in the middle of the pod.
	bodyCenter     = 0.15
	bodyHalfWidth  = modelWidth / 2
	bodyHalfHeight = modelHeight/2 - bodyCenter
	// sectionPower is the exponent of the superellipse of a section. 2 is an
	// ellipse, and a larger value gives flatter sides.
	sectionPower = 2.6
	// noseLength is the length of the rounded nose at each end.
	noseLength = 0.9
	// sectionPoints is the number of points around a section.
	sectionPoints = 32
	// gearHeight is the height of the running gear below the body.
	gearHeight = modelHeight/2 - bodyHalfHeight + bodyCenter
	// cabinInset is the distance from the outer surface to the cabin
	// surface.
	cabinInset = 0.04
)

// The windows. A face of the middle body is glass when its center is
// between the belt line and the roof line and outside the pillars. A nose
// is a glass dome above a belt line that goes down by noseBeltDrop from the
// start of the nose to the tip, so the rider sees the beam ahead. A nose
// face uses the height of its center on the full-size section, so the
// lower edge of the dome runs straight to the tip.
const (
	beltLine = -0.1
	roofLine = 0.8
	// pillarHalfWidth is half the length of a pillar along the pod.
	pillarHalfWidth = 0.06
	// pillarInset is the distance of the end pillars from the start of each
	// nose.
	pillarInset = 0.38
	// noseBeltDrop is how far the belt line of a nose goes down to the tip,
	// on the full-size section.
	noseBeltDrop = 0.5
)

// eye is the seat eye of the 4 m model. The ride page places its camera
// there.
var eye = vec3{0, 0.30, 0.5}

type vec3 struct{ X, Y, Z float64 }

func (a vec3) add(b vec3) vec3      { return vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func (a vec3) sub(b vec3) vec3      { return vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func (a vec3) scale(s float64) vec3 { return vec3{a.X * s, a.Y * s, a.Z * s} }
func (a vec3) dot(b vec3) float64   { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }
func (a vec3) length() float64      { return math.Sqrt(a.dot(a)) }
func (a vec3) unit() vec3           { return a.scale(1 / a.length()) }
func (a vec3) cross(b vec3) vec3 {
	return vec3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}
func (a vec3) mean(b, c vec3) vec3   { return a.add(b).add(c).scale(1.0 / 3) }
func (a vec3) arr() [3]float64       { return [3]float64{a.X, a.Y, a.Z} }
func signedPow(v, p float64) float64 { return math.Copysign(math.Pow(math.Abs(v), p), v) }

// part is one named mesh of the model with one material.
type part struct {
	name      string
	positions []vec3
	normals   []vec3
	uvs       [][2]float64
	indices   []uint16
	// project gives the texture coordinates of a vertex at position on a
	// triangle with the face normal face. A part with no projection has
	// zero texture coordinates.
	project func(position, face vec3) [2]float64
	// vertices maps a position, a normal, and texture coordinates, as the
	// file stores them, to the index of the vertex, so that triangles share
	// vertices.
	vertices map[[8]float32]uint16
}

// addTriangle adds the triangle a, b, c with the vertex normals na, nb, nc.
// The front face winds counterclockwise, as glTF requires.
func (p *part) addTriangle(a, b, c, na, nb, nc vec3) {
	face := b.sub(a).cross(c.sub(a))
	p.indices = append(p.indices, p.vertex(a, na, face), p.vertex(b, nb, face), p.vertex(c, nc, face))
}

// vertex returns the index of the vertex at position with normal, on a
// triangle with the face normal face, and adds the vertex if the part does
// not have it.
func (p *part) vertex(position, normal, face vec3) uint16 {
	var uv [2]float64
	if p.project != nil {
		uv = p.project(position, face)
	}
	key := [8]float32{
		float32(position.X), float32(position.Y), float32(position.Z),
		float32(normal.X), float32(normal.Y), float32(normal.Z),
		float32(uv[0]), float32(uv[1]),
	}
	if index, ok := p.vertices[key]; ok {
		return index
	}
	if p.vertices == nil {
		p.vertices = make(map[[8]float32]uint16)
	}
	if len(p.positions) > math.MaxUint16 {
		panic("podmodel: part " + p.name + " has more vertices than 16-bit indices allow")
	}
	index := uint16(len(p.positions)) // #nosec G115 -- Checked above.
	p.vertices[key] = index
	p.positions = append(p.positions, position)
	p.normals = append(p.normals, normal)
	p.uvs = append(p.uvs, uv)
	return index
}

// model holds the parts of the pod, the seat eye, and the screens of the
// cabin.
type model struct {
	parts   []part
	eye     vec3
	screens []screen
}

// The texture coordinates. The shell has u along the pod and v up, with
// one texture repeat for each shellTile meters of length and one for the
// model height. The center row of the texture, v = 0.5, is at
// stripeHeight, so the livery stripe of the texture runs around the pod
// below the windows. The cabin and the seats use a box projection.
const (
	shellTile    = 1.0
	stripeHeight = -0.45
	cabinTile    = 0.5
	seatTile     = 0.25
)

// shellProjection gives the shell texture coordinates of a position. They
// depend on the position only, so the shell vertices do not split.
func shellProjection(position, _ vec3) [2]float64 {
	return [2]float64{position.Z / shellTile, 0.5 + (position.Y-stripeHeight)/modelHeight}
}

// boxProjection returns a projection onto the plane of the main axis of
// the face normal, with one texture repeat for each tile meters.
func boxProjection(tile float64) func(position, face vec3) [2]float64 {
	return func(position, face vec3) [2]float64 {
		x, y, z := math.Abs(face.X), math.Abs(face.Y), math.Abs(face.Z)
		switch {
		case x >= y && x >= z:
			return [2]float64{position.Z / tile, position.Y / tile}
		case y >= z:
			return [2]float64{position.X / tile, position.Z / tile}
		default:
			return [2]float64{position.X / tile, position.Y / tile}
		}
	}
}

// quat is a rotation as a unit quaternion, in the component order of glTF.
type quat struct{ X, Y, Z, W float64 }

// axisAngle returns the rotation by angle radians about the unit axis.
func axisAngle(axis vec3, angle float64) quat {
	s := math.Sin(angle / 2)
	return quat{axis.X * s, axis.Y * s, axis.Z * s, math.Cos(angle / 2)}
}

// mul returns the rotation b followed by the rotation q.
func (q quat) mul(b quat) quat {
	return quat{
		q.W*b.X + q.X*b.W + q.Y*b.Z - q.Z*b.Y,
		q.W*b.Y - q.X*b.Z + q.Y*b.W + q.Z*b.X,
		q.W*b.Z + q.X*b.Y - q.Y*b.X + q.Z*b.W,
		q.W*b.W - q.X*b.X - q.Y*b.Y - q.Z*b.Z,
	}
}

// rotate returns v turned by q.
func (q quat) rotate(v vec3) vec3 {
	u := vec3{q.X, q.Y, q.Z}
	t := u.cross(v).scale(2)
	return v.add(t.scale(q.W)).add(u.cross(t))
}

func (q quat) arr() [4]float64 { return [4]float64{q.X, q.Y, q.Z, q.W} }

// screen is a flat display in the cabin, width by height meters, centered
// at center. In its own frame, the screen is the unit square from -0.5 to
// 0.5 in x and y, scaled by width and height, with +y up and its face
// toward -z. rotation turns that frame into the model frame. The ride page
// draws the readouts on the screens.
type screen struct {
	name          string
	center        vec3
	rotation      quat
	width, height float64
}

// facingEye returns a screen of width by height meters, distance meters
// from the eye. azimuth turns the direction from ahead toward +x, which is
// the left of the rider, and elevation turns it up. The screen faces the
// eye.
func facingEye(name string, azimuth, elevation, distance, width, height float64) screen {
	direction := vec3{math.Sin(azimuth) * math.Cos(elevation), math.Sin(elevation), math.Cos(azimuth) * math.Cos(elevation)}
	rotation := axisAngle(vec3{0, 1, 0}, azimuth).mul(axisAngle(vec3{1, 0, 0}, -elevation))
	return screen{name: name, center: eye.add(direction.scale(distance)), rotation: rotation, width: width, height: height}
}

// The screens of the cabin. Three screens stand on the dashboard below the
// sight line ahead: the mini-map on the left, the speed in the middle, and
// the route on the right. The banner is a head-up display at the top of
// the front window, below the sight line up through the front dome.
var cabinScreens = []screen{
	facingEye("mapScreen", 31*degree, -20*degree, 0.6, 0.18, 0.18),
	facingEye("speedScreen", 0, -23*degree, 0.6, 0.27, 0.16),
	facingEye("routeScreen", -31*degree, -20*degree, 0.6, 0.24, 0.16),
	facingEye("banner", 0, 19*degree, 0.85, 0.6, 0.12),
}

const degree = math.Pi / 180

// screenBorder is the width of the frame around a dashboard screen, and
// screenStand is how far the frame reaches below the screen, into the
// dashboard.
const (
	screenBorder = 0.012
	screenStand  = 0.08
	screenDepth  = 0.03
)

// addScreenFrame adds the frame and the stand of a dashboard screen to p,
// a little behind the screen face.
func addScreenFrame(p *part, s screen) {
	axes := [3]vec3{s.rotation.rotate(vec3{1, 0, 0}), s.rotation.rotate(vec3{0, 1, 0}), s.rotation.rotate(vec3{0, 0, 1})}
	low := vec3{-s.width/2 - screenBorder, -s.height/2 - screenStand, 0.002}
	high := vec3{s.width/2 + screenBorder, s.height/2 + screenBorder, screenDepth}
	addOrientedBox(p, s.center, axes, low, high)
}

// noseRings are the nose rings, from the start of the nose (0) to the last
// ring before the tip, as fractions of the nose length.
var noseRings = []float64{0, 0.3, 0.6, 0.75, 0.9, 0.98}

// ringPositions returns the z position and the nose fraction of each ring,
// from the rear to the front. The middle rings are 0.1 m apart. Each
// pillar edge also has a ring, so a pillar has its own faces.
func ringPositions() (zs, noses []float64) {
	half := modelLength/2 - noseLength
	fractions := append(slices.Clone(noseRings), (pillarInset-pillarHalfWidth)/noseLength, (pillarInset+pillarHalfWidth)/noseLength)
	slices.Sort(fractions)
	middle := []float64{-pillarHalfWidth, pillarHalfWidth}
	steps := int(math.Round(2 * half / 0.1))
	for i := 0; i <= steps; i++ {
		middle = append(middle, -half+2*half*float64(i)/float64(steps))
	}
	slices.Sort(middle)
	for i := len(fractions) - 1; i > 0; i-- {
		zs = append(zs, -half-noseLength*fractions[i])
		noses = append(noses, fractions[i])
	}
	for _, z := range middle {
		zs = append(zs, z)
		noses = append(noses, 0)
	}
	for _, fraction := range fractions[1:] {
		zs = append(zs, half+noseLength*fraction)
		noses = append(noses, fraction)
	}
	return zs, noses
}

// sectionPoint returns point j of the section at z with the nose fraction
// nose. A nose section shrinks toward the section center on an ellipse.
func sectionPoint(j int, z, nose float64) vec3 {
	shrink := math.Sqrt(1 - nose*nose)
	angle := 2 * math.Pi * float64(j) / sectionPoints
	x := signedPow(math.Cos(angle), 2/sectionPower) * bodyHalfWidth * shrink
	y := signedPow(math.Sin(angle), 2/sectionPower) * bodyHalfHeight * shrink
	return vec3{x, bodyCenter + y, z}
}

// isGlass reports whether the body face with center c is glass.
func isGlass(c vec3) bool {
	half := modelLength/2 - noseLength
	if nose := (math.Abs(c.Z) - half) / noseLength; nose > 0 {
		shrink := math.Sqrt(1 - min(nose, 1)*min(nose, 1))
		if shrink == 0 || bodyCenter+(c.Y-bodyCenter)/shrink <= beltLine-noseBeltDrop*nose {
			return false
		}
	} else if c.Y <= beltLine || c.Y >= roofLine {
		return false
	}
	end := half + pillarInset
	pillars := []float64{0, end, -end}
	for _, pillar := range pillars {
		if math.Abs(c.Z-pillar) < pillarHalfWidth {
			return false
		}
	}
	return true
}

// buildModel returns the pod model.
func buildModel() model {
	zs, noses := ringPositions()
	rings := make([][]vec3, len(zs))
	for i, z := range zs {
		for j := range sectionPoints {
			rings[i] = append(rings[i], sectionPoint(j, z, noses[i]))
		}
	}
	tips := [2]vec3{{0, bodyCenter, -modelLength / 2}, {0, bodyCenter, modelLength / 2}}
	normals := loftNormals(rings, tips)
	shell := part{name: "shell", project: shellProjection}
	glass := part{name: "glass"}
	cabin := part{name: "cabin", project: boxProjection(cabinTile)}
	for i := 0; i+1 < len(rings); i++ {
		for j := range sectionPoints {
			k := (j + 1) % sectionPoints
			quad := [4]vec3{rings[i][j], rings[i][k], rings[i+1][k], rings[i+1][j]}
			quadNormals := [4]vec3{normals[i][j], normals[i][k], normals[i+1][k], normals[i+1][j]}
			center := quad[0].add(quad[2]).scale(0.5)
			target := &shell
			if isGlass(center) {
				target = &glass
			}
			// The section runs counterclockwise seen from the front, so
			// the quad winds counterclockwise seen from outside.
			target.addTriangle(quad[0], quad[1], quad[2], quadNormals[0], quadNormals[1], quadNormals[2])
			target.addTriangle(quad[0], quad[2], quad[3], quadNormals[0], quadNormals[2], quadNormals[3])
			if target == &shell {
				addInside(&cabin, quad, quadNormals)
			}
		}
	}
	addTips(tipParts{shell: &shell, glass: &glass, cabin: &cabin}, rings, normals, tips)
	// The gear goes into the body by less than cabinInset, so it closes the
	// gap under the body but does not show in the cabin.
	gear := part{name: "gear"}
	addBox(&gear, vec3{-0.56, -modelHeight / 2, -0.35 * modelLength}, vec3{0.56, -modelHeight/2 + gearHeight + cabinInset/2, 0.35 * modelLength})
	// The dashboard is below the belt line, in front of the seat. The
	// screens stand on it.
	addBox(&cabin, vec3{-0.5, -0.4, eye.Z + 0.35}, vec3{0.5, -0.05, eye.Z + 0.85})
	for _, s := range cabinScreens {
		if s.name != "banner" {
			addScreenFrame(&cabin, s)
		}
	}
	return model{parts: []part{shell, glass, gear, cabin, buildSeats()}, eye: eye, screens: cabinScreens}
}

// buildSeats returns the seats: the seat of the rider at the front, under
// and behind the eye, and a bench at the rear that faces forward. The
// backs stay below the eye, so the rider sees out of the rear window.
func buildSeats() part {
	seat := part{name: "seat", project: boxProjection(seatTile)}
	for _, z := range []float64{0.42, -0.6} {
		addBox(&seat, vec3{-0.35, -0.68, z - 0.4}, vec3{0.35, -0.48, z - 0.05})
		addBox(&seat, vec3{-0.5, -0.48, z - 0.47}, vec3{0.5, -0.36, z})
		addBox(&seat, vec3{-0.5, -0.36, z - 0.57}, vec3{0.5, 0.12, z - 0.47})
	}
	return seat
}

// addInside adds the quad, moved in by cabinInset and turned to face the
// inside, to the cabin.
func addInside(cabin *part, quad, normals [4]vec3) {
	var inner [4]vec3
	for i := range quad {
		inner[i] = quad[i].sub(normals[i].scale(cabinInset))
	}
	flip := func(n vec3) vec3 { return n.scale(-1) }
	cabin.addTriangle(inner[0], inner[2], inner[1], flip(normals[0]), flip(normals[2]), flip(normals[1]))
	cabin.addTriangle(inner[0], inner[3], inner[2], flip(normals[0]), flip(normals[3]), flip(normals[2]))
}

// tipParts are the parts that the tip fans add to.
type tipParts struct{ shell, glass, cabin *part }

// addTips closes each end of the loft with a fan to its tip. A fan
// triangle is glass or shell by its center, and a shell triangle also adds
// its inside to the cabin.
func addTips(parts tipParts, rings, normals [][]vec3, tips [2]vec3) {
	ends := []struct {
		ring, tip int
		front     bool
	}{{0, 0, false}, {len(rings) - 1, 1, true}}
	for _, end := range ends {
		ring, tip := rings[end.ring], tips[end.tip]
		tipNormal := vec3{0, 0, -1}
		if end.front {
			tipNormal = vec3{0, 0, 1}
		}
		for j := range sectionPoints {
			k := (j + 1) % sectionPoints
			a, b := ring[j], ring[k]
			na, nb := normals[end.ring][j], normals[end.ring][k]
			if !end.front {
				a, b, na, nb = b, a, nb, na
			}
			if isGlass(a.mean(b, tip)) {
				parts.glass.addTriangle(a, b, tip, na, nb, tipNormal)
				continue
			}
			parts.shell.addTriangle(a, b, tip, na, nb, tipNormal)
			inset := func(p, n vec3) vec3 { return p.sub(n.scale(cabinInset)) }
			parts.cabin.addTriangle(inset(a, na), inset(tip, tipNormal), inset(b, nb), na.scale(-1), tipNormal.scale(-1), nb.scale(-1))
		}
	}
}

// loftNormals returns a smooth normal at each ring point: the sum of the
// normals of the faces around it, as a unit vector.
func loftNormals(rings [][]vec3, tips [2]vec3) [][]vec3 {
	sums := make([][]vec3, len(rings))
	for i := range rings {
		sums[i] = make([]vec3, sectionPoints)
	}
	for i := 0; i+1 < len(rings); i++ {
		for j := range sectionPoints {
			k := (j + 1) % sectionPoints
			n := rings[i][k].sub(rings[i][j]).cross(rings[i+1][j].sub(rings[i][j]))
			for _, at := range [][2]int{{i, j}, {i, k}, {i + 1, j}, {i + 1, k}} {
				sums[at[0]][at[1]] = sums[at[0]][at[1]].add(n)
			}
		}
	}
	last := len(rings) - 1
	for j := range sectionPoints {
		k := (j + 1) % sectionPoints
		// The face normals of the tip triangles, wound as addTips adds them.
		rear := rings[0][j].sub(rings[0][k]).cross(tips[0].sub(rings[0][k]))
		front := rings[last][k].sub(rings[last][j]).cross(tips[1].sub(rings[last][j]))
		sums[0][j], sums[0][k] = sums[0][j].add(rear), sums[0][k].add(rear)
		sums[last][j], sums[last][k] = sums[last][j].add(front), sums[last][k].add(front)
	}
	for i := range sums {
		for j := range sums[i] {
			sums[i][j] = sums[i][j].unit()
		}
	}
	return sums
}

// addBox adds the box from low to high with flat faces.
func addBox(p *part, low, high vec3) {
	addOrientedBox(p, vec3{}, [3]vec3{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}, low, high)
}

// addOrientedBox adds the box from low to high in the frame at origin with
// the unit axes, with flat faces. The axes must be a rotation of the model
// axes.
func addOrientedBox(p *part, origin vec3, axes [3]vec3, low, high vec3) {
	toModel := func(v vec3) vec3 { return axes[0].scale(v.X).add(axes[1].scale(v.Y)).add(axes[2].scale(v.Z)) }
	corner := func(x, y, z int) vec3 {
		pick := func(bit int, l, h float64) float64 {
			if bit == 0 {
				return l
			}
			return h
		}
		return vec3{pick(x, low.X, high.X), pick(y, low.Y, high.Y), pick(z, low.Z, high.Z)}
	}
	// Each face lists its corners counterclockwise seen from outside.
	faces := []struct {
		normal  vec3
		corners [4][3]int
	}{
		{vec3{1, 0, 0}, [4][3]int{{1, 0, 1}, {1, 0, 0}, {1, 1, 0}, {1, 1, 1}}},
		{vec3{-1, 0, 0}, [4][3]int{{0, 0, 0}, {0, 0, 1}, {0, 1, 1}, {0, 1, 0}}},
		{vec3{0, 1, 0}, [4][3]int{{0, 1, 1}, {1, 1, 1}, {1, 1, 0}, {0, 1, 0}}},
		{vec3{0, -1, 0}, [4][3]int{{0, 0, 0}, {1, 0, 0}, {1, 0, 1}, {0, 0, 1}}},
		{vec3{0, 0, 1}, [4][3]int{{0, 0, 1}, {1, 0, 1}, {1, 1, 1}, {0, 1, 1}}},
		{vec3{0, 0, -1}, [4][3]int{{1, 0, 0}, {0, 0, 0}, {0, 1, 0}, {1, 1, 0}}},
	}
	for _, face := range faces {
		var q [4]vec3
		for i, c := range face.corners {
			q[i] = origin.add(toModel(corner(c[0], c[1], c[2])))
		}
		n := toModel(face.normal)
		p.addTriangle(q[0], q[1], q[2], n, n, n)
		p.addTriangle(q[0], q[2], q[3], n, n, n)
	}
}
