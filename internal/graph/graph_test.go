package graph

import (
	"reflect"
	"testing"
)

func n(id string, parents ...string) Node { return Node{ID: id, Parents: parents} }

func TestLinearHistoryStaysInOneLane(t *testing.T) {
	rows := Layout([]Node{n("c", "b"), n("b", "a"), n("a")})
	for i, r := range rows {
		if r.Lane != 0 || r.Color != 0 || r.Width != 1 {
			t.Fatalf("row %d = %+v, want lane 0 color 0 width 1", i, r)
		}
	}
	if got := rows[0].Segments; !reflect.DeepEqual(got, []Segment{{0, Mid, 0, Bottom, 0, false}}) {
		t.Errorf("tip segments = %v", got)
	}
	if got := rows[1].Segments; !reflect.DeepEqual(got, []Segment{{0, Top, 0, Mid, 0, false}, {0, Mid, 0, Bottom, 0, false}}) {
		t.Errorf("middle segments = %v", got)
	}
	if got := rows[2].Segments; !reflect.DeepEqual(got, []Segment{{0, Top, 0, Mid, 0, false}}) {
		t.Errorf("root segments = %v", got)
	}
}

// m merges feature f into main:
//
//	m      (parents b, f)
//	| f
//	b |
//	|/
//	a
func TestMergeOpensAndClosesALane(t *testing.T) {
	rows := Layout([]Node{n("m", "b", "f"), n("f", "a"), n("b", "a"), n("a")})

	if rows[0].Lane != 0 {
		t.Fatalf("merge lane = %d", rows[0].Lane)
	}
	wantMerge := []Segment{{0, Mid, 0, Bottom, 0, false}, {0, Mid, 1, Bottom, 1, false}}
	if !reflect.DeepEqual(rows[0].Segments, wantMerge) {
		t.Errorf("merge segments = %v, want %v", rows[0].Segments, wantMerge)
	}
	if rows[1].Lane != 1 || rows[1].Color != 1 {
		t.Errorf("feature row = %+v, want lane 1 color 1", rows[1])
	}
	if rows[2].Lane != 0 || rows[2].Color != 0 {
		t.Errorf("main row = %+v", rows[2])
	}
	// Both lanes wait for a; it takes lane 0 and lane 1 bends into it.
	wantBase := []Segment{{0, Top, 0, Mid, 0, false}, {1, Top, 0, Mid, 1, false}}
	if !reflect.DeepEqual(rows[3].Segments, wantBase) {
		t.Errorf("base segments = %v, want %v", rows[3].Segments, wantBase)
	}
	if rows[3].Width != 2 {
		t.Errorf("base width = %d", rows[3].Width)
	}
}

func TestSeparateTipsGetSeparateLanesAndColors(t *testing.T) {
	// Two branches x and y share parent a.
	rows := Layout([]Node{n("x", "a"), n("y", "a"), n("a")})
	if rows[0].Lane != 0 || rows[1].Lane != 1 {
		t.Fatalf("tips in lanes %d and %d", rows[0].Lane, rows[1].Lane)
	}
	if rows[0].Color == rows[1].Color {
		t.Errorf("both tips have color %d", rows[0].Color)
	}
	// y's row: x's lane passes by.
	if !reflect.DeepEqual(rows[1].Segments[0], Segment{0, Top, 0, Bottom, rows[0].Color, false}) {
		t.Errorf("passing lane = %v", rows[1].Segments[0])
	}
}

func TestFreedLanesAreReused(t *testing.T) {
	// b2 branches off and ends (root) before c2 appears: c2 can take lane 1 again.
	rows := Layout([]Node{n("a", "b"), n("x"), n("b", "c"), n("y"), n("c")})
	if rows[1].Lane != 1 || rows[3].Lane != 1 {
		t.Errorf("lanes = %d, %d, want 1, 1", rows[1].Lane, rows[3].Lane)
	}
}

func TestMergeIntoExistingLane(t *testing.T) {
	// f is waited on by lane 1 (tip t) and merged by m in lane 0.
	rows := Layout([]Node{n("t", "f"), n("m", "a", "f"), n("f", "a"), n("a")})
	if rows[1].Lane != 1 {
		t.Fatalf("m lane = %d", rows[1].Lane)
	}
	// m's merge parent f is already awaited in lane 0 (by t), so it joins that lane.
	found := false
	for _, s := range rows[1].Segments {
		if s == (Segment{1, Mid, 0, Bottom, rows[0].Color, false}) {
			found = true
		}
	}
	if !found {
		t.Errorf("m segments %v lack a join into lane 0", rows[1].Segments)
	}
}

func TestDashedEdgesFollowTheirNode(t *testing.T) {
	// u (unpushed) skips past the side tip x to its parent p: the edge stays
	// dashed in every row it crosses, and p's own edge is solid again.
	rows := Layout([]Node{
		{ID: "u", Parents: []string{"p"}, Dashed: true},
		{ID: "x", Parents: []string{"q"}},
		{ID: "p", Parents: []string{"q"}},
		{ID: "q"},
	})
	want := []Segment{{0, Mid, 0, Bottom, 0, true}}
	if !reflect.DeepEqual(rows[0].Segments, want) {
		t.Errorf("u segments = %v, want %v", rows[0].Segments, want)
	}
	if s := rows[1].Segments[0]; s != (Segment{0, Top, 0, Bottom, 0, true}) {
		t.Errorf("u's edge passing x = %v, want dashed", s)
	}
	if s := rows[2].Segments[0]; s != (Segment{0, Top, 0, Mid, 0, true}) {
		t.Errorf("u's edge into p = %v, want dashed", s)
	}
	for _, s := range rows[2].Segments {
		if s.Y1 == Mid && s.Dashed {
			t.Errorf("p's own edge %v is dashed", s)
		}
	}
}
