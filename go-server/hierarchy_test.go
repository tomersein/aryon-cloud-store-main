package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func parseInput(t *testing.T, body string) *nodeInput {
	t.Helper()
	var n nodeInput
	if err := json.Unmarshal([]byte(body), &n); err != nil {
		t.Fatalf("bad test JSON: %v", err)
	}
	return &n
}

func TestFlattenRecordsParentsAndPositions(t *testing.T) {
	rows, err := flatten(parseInput(t, `{"id":1,"type":"management_group","children":[
		{"id":3,"type":"subscription","children":[{"id":5,"type":"resource_group","children":[]}]},
		{"id":2,"type":"subscription","children":[]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if rows.ids[0] != 1 {
		t.Fatalf("root must come first, got %v", rows.ids)
	}

	got := map[int64][2]int64{}
	for i, id := range rows.ids {
		got[id] = [2]int64{rows.parents[i], int64(rows.positions[i])}
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
			_, err := flatten(parseInput(t, tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestBuildTree(t *testing.T) {
	t.Run("no rows means not found", func(t *testing.T) {
		root, err := buildTree(nil)
		if root != nil || err != nil {
			t.Fatalf("got %v, %v", root, err)
		}
	})

	t.Run("leaves serialize with empty children", func(t *testing.T) {
		root, err := buildTree([]nodeRow{{ID: 1, Type: "subscription"}})
		if err != nil {
			t.Fatal(err)
		}
		out, _ := json.Marshal(root)
		if string(out) != `{"id":1,"type":"subscription","children":[]}` {
			t.Fatalf("got %s", out)
		}
	})

	t.Run("orphan row is an error", func(t *testing.T) {
		_, err := buildTree([]nodeRow{{ID: 1, Type: "subscription"}, {ID: 2, Type: "subscription", ParentID: 99}})
		if err == nil {
			t.Fatal("expected an error")
		}
	})
}

// asStored orders flattened rows the way the GET query returns them.
func asStored(rows *flatRows) []nodeRow {
	depth := map[int64]int{}
	parentOf := map[int64]int64{}
	for i, id := range rows.ids {
		parentOf[id] = rows.parents[i]
	}
	var depthOf func(id int64) int
	depthOf = func(id int64) int {
		if id == rows.ids[0] {
			return 0
		}
		if d, ok := depth[id]; ok {
			return d
		}
		depth[id] = depthOf(parentOf[id]) + 1
		return depth[id]
	}

	idx := make([]int, len(rows.ids))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool {
		ia, ib := idx[a], idx[b]
		da, db := depthOf(rows.ids[ia]), depthOf(rows.ids[ib])
		if da != db {
			return da < db
		}
		if rows.parents[ia] != rows.parents[ib] {
			return rows.parents[ia] < rows.parents[ib]
		}
		return rows.positions[ia] < rows.positions[ib]
	})

	out := make([]nodeRow, len(idx))
	for i, j := range idx {
		out[i] = nodeRow{ID: rows.ids[j], Type: rows.types[j], ParentID: rows.parents[j]}
	}
	return out
}

func TestRoundTripOfProvidedObjects(t *testing.T) {
	files, err := filepath.Glob("../tests/objects/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no test objects found: %v", err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			original, err := os.ReadFile(file) //nolint:gosec // fixed test fixtures
			if err != nil {
				t.Fatal(err)
			}
			rows, err := flatten(parseInput(t, string(original)))
			if err != nil {
				t.Fatal(err)
			}
			root, err := buildTree(asStored(rows))
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
