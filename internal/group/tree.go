package group

import "github.com/kiraa06/rwnd/internal/scan"

const maxTreeDepth = 4

// Node is a session in tree-walk order, with the indent the picker should use.
type Node struct {
	Session scan.Session
	Depth   int
	Last    bool // last sibling at this depth; drives └─ vs ├─
}

// Order nests forks under the parent when that parent is in the same slice.
// Sessions whose parent is missing, in another section, or would form a cycle
// stay roots. Roots and siblings stay newest-first (the incoming order).
func Order(sessions []scan.Session) []Node {
	if len(sessions) == 0 {
		return nil
	}

	index := make(map[string]int, len(sessions))
	for i, s := range sessions {
		if s.ID != "" {
			index[s.ID] = i
		}
	}

	children := make(map[string][]int)
	isChild := make([]bool, len(sessions))
	for i, s := range sessions {
		if s.ParentID == "" {
			continue
		}
		p, ok := index[s.ParentID]
		if !ok || p == i {
			continue
		}
		children[s.ParentID] = append(children[s.ParentID], i)
		isChild[i] = true
	}

	out := make([]Node, 0, len(sessions))
	emitted := make([]bool, len(sessions))
	onPath := make([]bool, len(sessions))
	var walk func(i, depth int, last bool)
	walk = func(i, depth int, last bool) {
		if emitted[i] || onPath[i] {
			return
		}
		if depth > maxTreeDepth {
			depth = maxTreeDepth
		}
		onPath[i] = true
		emitted[i] = true
		out = append(out, Node{Session: sessions[i], Depth: depth, Last: last})
		kids := children[sessions[i].ID]
		lastWalkable := -1
		for k, ci := range kids {
			if !emitted[ci] && !onPath[ci] {
				lastWalkable = k
			}
		}
		for k, ci := range kids {
			if emitted[ci] || onPath[ci] {
				continue
			}
			walk(ci, depth+1, k == lastWalkable)
		}
		onPath[i] = false
	}

	for i := range sessions {
		if isChild[i] {
			continue
		}
		walk(i, 0, false)
	}
	// A cycle with no external root would be skipped above; emit leftovers.
	for i := range sessions {
		if !emitted[i] {
			walk(i, 0, false)
		}
	}
	return out
}

// CapRoots keeps the first cap root sessions and every nested child of those
// roots. Hidden is the number of omitted sessions (roots and their children).
func CapRoots(sessions []scan.Session, cap int) (kept []scan.Session, hidden int) {
	if cap <= 0 || len(sessions) <= cap {
		return sessions, 0
	}
	nodes := Order(sessions)
	var groups [][]scan.Session
	for _, n := range nodes {
		if n.Depth == 0 || len(groups) == 0 {
			groups = append(groups, []scan.Session{n.Session})
			continue
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], n.Session)
	}
	if len(groups) <= cap {
		return sessions, 0
	}
	for _, g := range groups[:cap] {
		kept = append(kept, g...)
	}
	for _, g := range groups[cap:] {
		hidden += len(g)
	}
	return kept, hidden
}
