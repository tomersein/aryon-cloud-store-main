// Package hierarchy holds the tree model and the conversions between a JSON
// tree and the flat rows the store reads and writes.
package hierarchy

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
)

var (
	ErrCycle    = errors.New("the posted root's current ancestors cannot appear inside its subtree")
	ErrNotFound = errors.New("node not found")
)

type Node struct {
	ID       int64   `json:"id"`
	Type     string  `json:"type"`
	Children []*Node `json:"children"`
}

// NodeInput uses pointers so a missing field can be told apart from a zero value.
type NodeInput struct {
	ID       *int64       `json:"id"`
	Type     *string      `json:"type"`
	Children []*NodeInput `json:"children"`
}

var validTypes = map[string]bool{
	"management_group": true,
	"subscription":     true,
	"resource_group":   true,
}

// FlatRows holds the tree as parallel columns. IDs[0] is always the root.
// The root's parent is recorded as 0 and is never written.
type FlatRows struct {
	IDs       []int64
	Types     []string
	Parents   []int64
	Positions []int32
}

// NodeRow is one stored node, as read back from the database.
type NodeRow struct {
	ID       int64
	Type     string
	ParentID int64
	Position int32
}

func Flatten(root *NodeInput) (*FlatRows, error) {
	rows := &FlatRows{}
	seen := map[int64]bool{}

	type item struct {
		node     *NodeInput
		parent   int64
		position int32
	}
	stack := []item{{node: root}}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		n := it.node
		if n == nil || n.ID == nil || n.Type == nil {
			return nil, errors.New(`every node needs an "id" and a "type"`)
		}
		if !validTypes[*n.Type] {
			return nil, fmt.Errorf("node %d has unknown type %q", *n.ID, *n.Type)
		}
		if seen[*n.ID] {
			return nil, fmt.Errorf("node %d appears more than once", *n.ID)
		}
		seen[*n.ID] = true

		rows.IDs = append(rows.IDs, *n.ID)
		rows.Types = append(rows.Types, *n.Type)
		rows.Parents = append(rows.Parents, it.parent)
		rows.Positions = append(rows.Positions, it.position)

		for i, child := range n.Children {
			stack = append(stack, item{node: child, parent: *n.ID, position: int32(i)}) //nolint:gosec // child counts fit in int32
		}
	}
	return rows, nil
}

// BuildTree assembles the subtree under rootID from rows in any order.
// It returns nil when rootID is not among the rows.
func BuildTree(rootID int64, rows []NodeRow) (*Node, error) {
	byID := make(map[int64]*Node, len(rows))
	for _, r := range rows {
		byID[r.ID] = &Node{ID: r.ID, Type: r.Type, Children: []*Node{}}
	}
	root, ok := byID[rootID]
	if !ok {
		return nil, nil
	}

	sorted := slices.Clone(rows)
	slices.SortFunc(sorted, func(a, b NodeRow) int {
		return cmp.Or(cmp.Compare(a.ParentID, b.ParentID), cmp.Compare(a.Position, b.Position))
	})
	for _, r := range sorted {
		if r.ID == rootID {
			continue
		}
		parent, ok := byID[r.ParentID]
		if !ok {
			return nil, fmt.Errorf("node %d has parent %d outside the subtree", r.ID, r.ParentID)
		}
		parent.Children = append(parent.Children, byID[r.ID])
	}
	return root, nil
}
