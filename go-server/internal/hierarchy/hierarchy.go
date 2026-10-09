// Package hierarchy holds the tree model and the conversions between a JSON
// tree and the flat rows the store reads and writes.
package hierarchy

import (
	"errors"
	"fmt"
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

// FlatRows holds the tree as parallel columns, root first.
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

// BuildTree expects the root first and every parent before its children,
// with siblings in position order.
func BuildTree(rows []NodeRow) (*Node, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	byID := make(map[int64]*Node, len(rows))
	var root *Node
	for i, r := range rows {
		node := &Node{ID: r.ID, Type: r.Type, Children: []*Node{}}
		byID[r.ID] = node
		if i == 0 {
			root = node
			continue
		}
		parent, ok := byID[r.ParentID]
		if !ok {
			return nil, fmt.Errorf("node %d arrived before its parent %d", r.ID, r.ParentID)
		}
		parent.Children = append(parent.Children, node)
	}
	return root, nil
}
