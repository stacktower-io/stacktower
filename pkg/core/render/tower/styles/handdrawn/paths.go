package handdrawn

import (
	"bytes"
	"fmt"
	"math"
)

const (
	wobble       = 4.0
	segDensity   = 0.015
	minSegs      = 3
	maxEdgeShift = 6.0
	curveOffset  = 15.0
	curveMinDist = 50.0
)

func wobbledRect(x, y, w, h float64, seed uint64, id string) string {
	rng := newRNG(hash(id, seed))

	numH := max(minSegs, int(w*segDensity))
	numV := max(minSegs, int(h*segDensity))

	jitter := func(v float64) float64 {
		return v + (rng.next()*2-1)*wobble
	}

	pts := make([][2]float64, 0, (numH+numV)*2)

	for i := 0; i <= numH; i++ {
		t := float64(i) / float64(numH)
		pts = append(pts, [2]float64{x + t*w, jitter(y)})
	}
	for i := 1; i <= numV; i++ {
		t := float64(i) / float64(numV)
		pts = append(pts, [2]float64{jitter(x + w), y + t*h})
	}
	for i := 1; i <= numH; i++ {
		t := float64(i) / float64(numH)
		pts = append(pts, [2]float64{x + w - t*w, jitter(y + h)})
	}
	for i := 1; i < numV; i++ {
		t := float64(i) / float64(numV)
		pts = append(pts, [2]float64{jitter(x), y + h - t*h})
	}

	if len(pts) < 3 {
		return fmt.Sprintf("M %.2f %.2f h %.2f v %.2f h %.2f Z", x, y, w, h, -w)
	}

	var path bytes.Buffer
	last, first := pts[len(pts)-1], pts[0]
	fmt.Fprintf(&path, "M %.2f %.2f", (last[0]+first[0])/2, (last[1]+first[1])/2)

	for i := range pts {
		curr := pts[i]
		next := pts[(i+1)%len(pts)]
		fmt.Fprintf(&path, " Q %.2f %.2f %.2f %.2f", curr[0], curr[1], (curr[0]+next[0])/2, (curr[1]+next[1])/2)
	}

	path.WriteString(" Z")
	return path.String()
}

func curvedEdge(x1, y1, x2, y2 float64) string {
	dx, dy := x2-x1, y2-y1
	dist := math.Hypot(dx, dy)

	if dist < curveMinDist {
		return fmt.Sprintf("M %.2f %.2f L %.2f %.2f", x1, y1, x2, y2)
	}

	perpX := -dy / dist * curveOffset
	perpY := dx / dist * curveOffset

	return fmt.Sprintf("M %.2f %.2f C %.2f %.2f %.2f %.2f %.2f %.2f",
		x1, y1,
		x1+dx*0.33+perpX, y1+dy*0.33+perpY,
		x1+dx*0.67-perpX, y1+dy*0.67-perpY,
		x2, y2)
}

func rotationFor(id string, w, h float64) float64 {
	maxAngle := math.Atan(maxEdgeShift/max(w, h)) * 180 / math.Pi
	rng := newRNG(hash(id, 999))
	return (rng.next()*2 - 1) * maxAngle
}

// wobblyTriangle draws a hand-drawn triangular pennant with wobbly edges.
// The triangle is defined by three points: p1 (top-right), p2 (left tip), p3 (bottom-right).
// The right edge (p3 to p1) is kept straight to align with the pole.
func wobblyTriangle(x1, y1, x2, y2, x3, y3 float64, seed uint64) string {
	rng := newRNG(seed + 42)

	jitter := func(v float64) float64 {
		return v + (rng.next()*2-1)*wobble*0.4
	}

	// p1 and p3 are on the pole - keep their X exact, only jitter Y slightly
	p1x, p1y := x1, y1+jitter(0)
	p2x, p2y := jitter(x2), jitter(y2) // left tip can wobble freely
	p3x, p3y := x3, y3+jitter(0)

	// Draw with slight curve through control points for more hand-drawn feel
	var path bytes.Buffer
	// Start at first point (top of pole edge)
	fmt.Fprintf(&path, "M %.2f %.2f", p1x, p1y)

	// Edge from p1 to p2 (top edge going left) - wobbly
	mid12x := (p1x+p2x)/2 + (rng.next()*2-1)*2
	mid12y := (p1y+p2y)/2 + (rng.next()*2-1)*2
	fmt.Fprintf(&path, " Q %.2f %.2f %.2f %.2f", mid12x, mid12y, p2x, p2y)

	// Edge from p2 to p3 (bottom edge going right) - wobbly
	mid23x := (p2x+p3x)/2 + (rng.next()*2-1)*2
	mid23y := (p2y+p3y)/2 + (rng.next()*2-1)*2
	fmt.Fprintf(&path, " Q %.2f %.2f %.2f %.2f", mid23x, mid23y, p3x, p3y)

	// Edge from p3 back to p1 (along the pole) - STRAIGHT line
	fmt.Fprintf(&path, " L %.2f %.2f", p1x, p1y)

	path.WriteString(" Z")
	return path.String()
}

type rng struct{ state uint64 }

func newRNG(seed uint64) *rng { return &rng{state: seed} }

func (r *rng) next() float64 {
	r.state = r.state*6364136223846793005 + 1442695040888963407
	return float64(r.state>>32) / float64(1<<32)
}
