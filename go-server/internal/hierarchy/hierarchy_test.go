package hierarchy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
	t.Run("missing root means not found", func(t *testing.T) {
		root, err := BuildTree(1, nil)
		if root != nil || err != nil {
			t.Fatalf("got %v, %v", root, err)
		}
	})

	t.Run("leaves serialize with empty children", func(t *testing.T) {
		root, err := BuildTree(1, []NodeRow{{ID: 1, Type: "subscription"}})
		if err != nil {
			t.Fatal(err)
		}
		out, _ := json.Marshal(root)
		if string(out) != `{"id":1,"type":"subscription","children":[]}` {
			t.Fatalf("got %s", out)
		}
	})

	t.Run("rows in any order, siblings by position", func(t *testing.T) {
		root, err := BuildTree(1, []NodeRow{
			{ID: 5, Type: "resource_group", ParentID: 3, Position: 0},
			{ID: 2, Type: "subscription", ParentID: 1, Position: 1},
			{ID: 1, Type: "management_group", ParentID: 42},
			{ID: 3, Type: "subscription", ParentID: 1, Position: 0},
		})
		if err != nil {
			t.Fatal(err)
		}
		out, _ := json.Marshal(root)
		want := `{"id":1,"type":"management_group","children":[` +
			`{"id":3,"type":"subscription","children":[{"id":5,"type":"resource_group","children":[]}]},` +
			`{"id":2,"type":"subscription","children":[]}]}`
		if string(out) != want {
			t.Fatalf("got  %s\nwant %s", out, want)
		}
	})

	t.Run("orphan row is an error", func(t *testing.T) {
		_, err := BuildTree(1, []NodeRow{{ID: 1, Type: "subscription"}, {ID: 2, Type: "subscription", ParentID: 99}})
		if err == nil {
			t.Fatal("expected an error")
		}
	})
}

// asStored turns flattened rows into stored rows, in reverse to show order does not matter.
func asStored(rows *FlatRows) []NodeRow {
	out := make([]NodeRow, len(rows.IDs))
	for i := range rows.IDs {
		out[len(out)-1-i] = NodeRow{ID: rows.IDs[i], Type: rows.Types[i], ParentID: rows.Parents[i], Position: rows.Positions[i]}
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
			root, err := BuildTree(rows.IDs[0], asStored(rows))
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
