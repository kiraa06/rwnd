package group

import (
	"testing"
	"time"

	"github.com/kiraa06/rwnd/internal/scan"
)

func forkSess(id, parent, title string, age time.Duration) scan.Session {
	return scan.Session{
		ID:       id,
		ParentID: parent,
		Title:    title,
		Modified: time.Now().Add(-age),
	}
}

func nodeIDs(nodes []Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Session.ID
	}
	return out
}

func TestOrderNestsForksUnderParent(t *testing.T) {
	// Incoming order is newest-first, matching scan.All.
	sessions := []scan.Session{
		forkSess("child-new", "parent", "fork new", time.Hour),
		forkSess("parent", "", "main", 2*time.Hour),
		forkSess("child-old", "parent", "fork old", 3*time.Hour),
		forkSess("other", "", "unrelated", 4*time.Hour),
	}
	got := Order(sessions)
	want := []string{"parent", "child-new", "child-old", "other"}
	if !equal(nodeIDs(got), want) {
		t.Errorf("order = %v, want %v", nodeIDs(got), want)
	}
	if got[0].Depth != 0 || got[1].Depth != 1 || got[2].Depth != 1 || got[3].Depth != 0 {
		t.Errorf("depths = %d %d %d %d", got[0].Depth, got[1].Depth, got[2].Depth, got[3].Depth)
	}
	if !got[2].Last || got[1].Last {
		t.Errorf("last flags: child-new=%v child-old=%v, want false/true", got[1].Last, got[2].Last)
	}
}

func TestOrderOrphanIsRoot(t *testing.T) {
	sessions := []scan.Session{
		forkSess("child", "missing", "orphan", 0),
		forkSess("root", "", "root", time.Hour),
	}
	got := Order(sessions)
	if !equal(nodeIDs(got), []string{"child", "root"}) {
		t.Errorf("order = %v, want orphan then root", nodeIDs(got))
	}
	if got[0].Depth != 0 {
		t.Errorf("orphan depth = %d, want 0", got[0].Depth)
	}
}

func TestOrderLastIsVisuallyLastSibling(t *testing.T) {
	sessions := []scan.Session{
		forkSess("p", "c", "p", 0),
		forkSess("a", "p", "a", time.Hour),
		forkSess("c", "p", "c", 2*time.Hour),
		forkSess("b", "p", "b", 3*time.Hour),
	}
	got := Order(sessions)
	for i, n := range got {
		if n.Depth == 0 {
			continue
		}
		visuallyLast := i == len(got)-1 || got[i+1].Depth < n.Depth
		if visuallyLast != n.Last {
			t.Errorf("node %s depth %d Last=%v visuallyLast=%v order=%v",
				n.Session.ID, n.Depth, n.Last, visuallyLast, nodeIDs(got))
		}
	}
}

func TestOrderBreaksCycles(t *testing.T) {
	sessions := []scan.Session{
		forkSess("a", "b", "a", 0),
		forkSess("b", "a", "b", time.Hour),
	}
	got := Order(sessions)
	if len(got) != 2 {
		t.Fatalf("got %d nodes, want both sessions", len(got))
	}
	seen := map[string]bool{}
	for _, n := range got {
		if seen[n.Session.ID] {
			t.Fatalf("duplicated %s", n.Session.ID)
		}
		seen[n.Session.ID] = true
	}
}

func TestCapRootsKeepsChildrenOfVisibleParents(t *testing.T) {
	var sessions []scan.Session
	sessions = append(sessions, forkSess("p1", "", "p1", 0))
	sessions = append(sessions, forkSess("c1", "p1", "c1", time.Hour))
	sessions = append(sessions, forkSess("p2", "", "p2", 2*time.Hour))
	sessions = append(sessions, forkSess("c2", "p2", "c2", 3*time.Hour))
	sessions = append(sessions, forkSess("p3", "", "p3", 4*time.Hour))

	kept, hidden := CapRoots(sessions, 2)
	var got []string
	for _, s := range kept {
		got = append(got, s.ID)
	}
	if !equal(got, []string{"p1", "c1", "p2", "c2"}) {
		t.Errorf("kept = %v, want p1's and p2's trees", got)
	}
	if hidden != 1 {
		t.Errorf("hidden = %d, want 1 (p3)", hidden)
	}
}
