// Package graph assigns commits to lanes and computes the line pieces drawn in
// each row of the history graph.
//
// Every row is split into an upper half (row top to the commit dot) and a lower
// half (dot to row bottom). A segment connects two of three vertical positions in
// a row: Top, Mid (the dot's height) or Bottom. Rows are drawn independently, so
// the page can render any slice of the history without the rows around it.
package graph

// Vertical positions inside a row.
const (
	Top    = 0
	Mid    = 1
	Bottom = 2
)

// Segment is a line piece inside one row, from lane X1 at Y1 to lane X2 at Y2.
// Dashed segments belong to an edge that leaves a Dashed node.
type Segment struct {
	X1, Y1, X2, Y2 int
	Color          int
	Dashed         bool
}

// Row is the graph data for one commit.
type Row struct {
	Lane     int // column of the commit dot
	Color    int // color of the dot
	Segments []Segment
	Width    int // number of lanes this row uses, for placing the text after the graph
}

// Node is the minimum the layout needs to know about a commit.
type Node struct {
	ID      string
	Parents []string
	Dashed  bool // draw the edges to its parents dashed
}

type lane struct {
	id     string // commit this lane is waiting for
	color  int
	dashed bool // the edge in this lane leaves a Dashed node
}

// Layout computes rows for commits given newest first, where no parent appears
// before any of its children (git log --date-order or --topo-order).
//
// Each lane waits for one commit. A commit takes the leftmost lane waiting for it,
// and any other lanes waiting for it end in its dot (a branch point seen from the
// past). Its first parent continues in the same lane and color; further parents
// (merges) join a lane already waiting for them or open a new one.
func Layout(nodes []Node) []Row {
	rows := make([]Row, len(nodes))
	var lanes []*lane
	nextColor := 0
	freeLane := func() int {
		for i, l := range lanes {
			if l == nil {
				return i
			}
		}
		lanes = append(lanes, nil)
		return len(lanes) - 1
	}

	for i, n := range nodes {
		row := &rows[i]
		col := -1
		for j, l := range lanes {
			if l != nil && l.id == n.ID {
				col = j
				break
			}
		}
		tip := col < 0
		if tip { // nothing below points here yet: a branch tip
			col = freeLane()
			lanes[col] = &lane{id: n.ID, color: nextColor, dashed: n.Dashed}
			nextColor++
		}
		row.Lane = col
		row.Color = lanes[col].color

		// Upper half: lanes waiting for this commit end in its dot; the rest pass by.
		for j, l := range lanes {
			if l == nil {
				continue
			}
			switch {
			case l.id == n.ID:
				if !(tip && j == col) {
					row.Segments = append(row.Segments, Segment{j, Top, col, Mid, l.color, l.dashed})
				}
				if j != col {
					lanes[j] = nil
				}
			default:
				row.Segments = append(row.Segments, Segment{j, Top, j, Bottom, l.color, l.dashed})
			}
		}

		// Lower half: continue to the parents.
		if len(n.Parents) == 0 {
			lanes[col] = nil
		} else {
			lanes[col] = &lane{id: n.Parents[0], color: row.Color, dashed: n.Dashed}
			row.Segments = append(row.Segments, Segment{col, Mid, col, Bottom, row.Color, n.Dashed})
			for _, p := range n.Parents[1:] {
				k := -1
				for j, l := range lanes {
					if l != nil && l.id == p && j != col {
						k = j
						break
					}
				}
				if k < 0 {
					k = freeLane()
					lanes[k] = &lane{id: p, color: nextColor, dashed: n.Dashed}
					nextColor++
				}
				// Joining a lane that already waits keeps that lane's style below this row.
				row.Segments = append(row.Segments, Segment{col, Mid, k, Bottom, lanes[k].color, n.Dashed})
			}
		}

		for len(lanes) > 0 && lanes[len(lanes)-1] == nil {
			lanes = lanes[:len(lanes)-1]
		}
		row.Width = col + 1
		for _, s := range row.Segments {
			row.Width = max(row.Width, s.X1+1, s.X2+1)
		}
	}
	return rows
}
