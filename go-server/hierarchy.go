package main

import (
	"errors"
	"fmt"
)

type Node struct {
	ID       int64   `json:"id"`
	Type     string  `json:"type"`
	Children []*Node `json:"children"`
}

// nodeInput uses pointers so a missing field can be told apart from a zero value.
type nodeInput struct {
	ID       *int64       `json:"id"`
	Type     *string      `json:"type"`
	Children []*nodeInput `json:"children"`
}

var validTypes = map[string]bool{
	"management_group": true,
	"subscription":     true,
	"resource_group":   true,
}

// flatRows holds the tree as parallel columns, ready to pass to unnest().
// The root's parent is recorded as 0 and is never written.
type flatRows struct {
	ids       []int64
	types     []string
	parents   []int64
	positions []int32
}

// nodeRow is one stored node, as read back from the database.
type nodeRow struct {
	ID       int64
	Type     string
	ParentID int64
}

func flatten(root *nodeInput) (*flatRows, error) {
	rows := &flatRows{}
	seen := map[int64]bool{}

	type item struct {
		node     *nodeInput
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

		rows.ids = append(rows.ids, *n.ID)
		rows.types = append(rows.types, *n.Type)
		rows.parents = append(rows.parents, it.parent)
		rows.positions = append(rows.positions, it.position)

		for i, child := range n.Children {
			stack = append(stack, item{node: child, parent: *n.ID, position: int32(i)}) //nolint:gosec // child counts fit in int32
		}
	}
	return rows, nil
}

// buildTree expects the root first and every parent before its children,
// with siblings in position order.
func buildTree(rows []nodeRow) (*Node, error) {
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
