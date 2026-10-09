package hierarchy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func parseInput(t *testing.T, body string) *NodeInput {
	t.Helper()
	var n NodeInput
	if err := json.Unmarshal([]byte(body), &n); err != nil {
		t.Fatalf("bad test JSON: %v", err)
	}
	return &n
}

func TestFlattenRecordsParentsAndPositions(t *testing.T) {
	rows, err := Flatten(parseInput(t, `{"id":1,"type":"management_group","children":[
		{"id":3,"type":"subscription","children":[{"id":5,"type":"resource_group","children":[]}]},
		{"id":2,"type":"subscription","children":[]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if rows.IDs[0] != 1 {
		t.Fatalf("root must come first, got %v", rows.IDs)
	}

	got := map[int64][2]int64{}
	for i, id := range rows.IDs {
		got[id] = [2]int64{rows.Parents[i], int64(rows.Positions[i])}
	}
	want := map[int64][2]int64{1: {0, 0}, 3: {1, 0}, 2: {1, 1}, 5: {3, 0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parent/position = %v, want %v", got, want)
	}
}

func TestFlattenRejectsInvalidTrees(t *testing.T) {
	tests := map[string]struct {
		body    string
		wantErr string
	}{
		"missing id":      {`{"type":"subscription","children":[]}`, `needs an "id"`},
		"missing type":    {`{"id":1,"children":[]}`, `needs an "id" and a "type"`},
		"null child":      {`{"id":1,"type":"subscription","children":[null]}`, `needs an "id"`},
		"unknown type":    {`{"id":1,"type":"vm","children":[]}`, `unknown type "vm"`},
		"duplicate child": {`{"id":1,"type":"subscription","children":[{"id":1,"type":"subscription"}]}`, "node 1 appears more than once"},
		"duplicate across branches": {`{"id":1,"type":"subscription","children":[
			{"id":2,"type":"subscription","children":[{"id":9,"type":"resource_group"}]},
			{"id":3,"type":"subscription","children":[{"id":9,"type":"resource_group"}]}]}`, "node 9 appears more than once"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Flatten(parseInput(t, tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestBuildTree(t *testing.T) {
	t.Run("no rows means not found", func(t *testing.T) {
		root, err := BuildTree(nil)
		if root != nil || err != nil {
			t.Fatalf("got %v, %v", root, err)
		}
	})

	t.Run("leaves serialize with empty children", func(t *testing.T) {
		root, err := BuildTree([]NodeRow{{ID: 1, Type: "subscription"}})
		if err != nil {
			t.Fatal(err)
		}
		out, _ := json.Marshal(root)
		if string(out) != `{"id":1,"type":"subscription","children":[]}` {
			t.Fatalf("got %s", out)
		}
	})

	t.Run("orphan row is an error", func(t *testing.T) {
		_, err := BuildTree([]NodeRow{{ID: 1, Type: "subscription"}, {ID: 2, Type: "subscription", ParentID: 99}})
		if err == nil {
			t.Fatal("expected an error")
		}
	})
}

// asStored orders flattened rows the way the GET query returns them.
func asStored(rows *FlatRows) []NodeRow {
	depth := map[int64]int{}
	parentOf := map[int64]int64{}
	for i, id := range rows.IDs {
		parentOf[id] = rows.Parents[i]
	}
	var depthOf func(id int64) int
	depthOf = func(id int64) int {
		if id == rows.IDs[0] {
			return 0
		}
		if d, ok := depth[id]; ok {
			return d
		}
		depth[id] = depthOf(parentOf[id]) + 1
		return depth[id]
	}

	idx := make([]int, len(rows.IDs))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool {
		ia, ib := idx[a], idx[b]
		da, db := depthOf(rows.IDs[ia]), depthOf(rows.IDs[ib])
		if da != db {
			return da < db
		}
		if rows.Parents[ia] != rows.Parents[ib] {
			return rows.Parents[ia] < rows.Parents[ib]
		}
		return rows.Positions[ia] < rows.Positions[ib]
	})

	out := make([]NodeRow, len(idx))
	for i, j := range idx {
		out[i] = NodeRow{ID: rows.IDs[j], Type: rows.Types[j], ParentID: rows.Parents[j]}
	}
	return out
}

func TestRoundTripOfProvidedObjects(t *testing.T) {
	files, err := filepath.Glob("../../../tests/objects/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no test objects found: %v", err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			original, err := os.ReadFile(file) //nolint:gosec // fixed test fixtures
			if err != nil {
				t.Fatal(err)
			}
			rows, err := Flatten(parseInput(t, string(original)))
			if err != nil {
				t.Fatal(err)
			}
			root, err := BuildTree(asStored(rows))
			if err != nil {
				t.Fatal(err)
			}

			var want, got any
			_ = json.Unmarshal(original, &want)
			rebuilt, _ := json.Marshal(root)
			_ = json.Unmarshal(rebuilt, &got)
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("round trip changed the tree:\nwant %v\ngot  %v", want, got)
			}
		})
	}
}
