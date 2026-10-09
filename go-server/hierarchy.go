package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
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

// writeLockKey serializes writers; readers are never blocked.
const writeLockKey = 7340001

var errCycle = errors.New("the posted root's current ancestors cannot appear inside its subtree")

// flatRows holds the tree as parallel columns, ready to pass to unnest().
type flatRows struct {
	ids       []int64
	types     []string
	parents   []sql.NullInt64
	positions []int32
}

func flatten(root *nodeInput) (*flatRows, error) {
	rows := &flatRows{}
	seen := map[int64]bool{}

	type item struct {
		node     *nodeInput
		parent   sql.NullInt64
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
			stack = append(stack, item{
				node:     child,
				parent:   sql.NullInt64{Int64: *n.ID, Valid: true},
				position: int32(i),
			})
		}
	}
	return rows, nil
}

func storeHierarchy(db *sql.DB, rootID int64, rows *flatRows) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1)`, writeLockKey); err != nil {
		return err
	}

	// The root keeps its current parent, so it must not become its own descendant.
	var cycle bool
	err = tx.QueryRow(`
		WITH RECURSIVE ancestors AS (
			SELECT parent_id FROM nodes WHERE id = $1
			UNION
			SELECT n.parent_id FROM nodes n JOIN ancestors a ON n.id = a.parent_id
		)
		SELECT EXISTS (SELECT 1 FROM ancestors WHERE parent_id = ANY($2))`,
		rootID, pq.Array(rows.ids),
	).Scan(&cycle)
	if err != nil {
		return err
	}
	if cycle {
		return errCycle
	}

	_, err = tx.Exec(`
		INSERT INTO nodes (id, type, parent_id, position)
		SELECT * FROM unnest($1::bigint[], $2::text[], $3::bigint[], $4::int[])
		ON CONFLICT (id) DO UPDATE SET
			type      = EXCLUDED.type,
			parent_id = CASE WHEN nodes.id = $5 THEN nodes.parent_id ELSE EXCLUDED.parent_id END,
			position  = CASE WHEN nodes.id = $5 THEN nodes.position  ELSE EXCLUDED.position  END
		WHERE nodes.type IS DISTINCT FROM EXCLUDED.type
		   OR (nodes.id <> $5 AND (nodes.parent_id, nodes.position)
		       IS DISTINCT FROM (EXCLUDED.parent_id, EXCLUDED.position))`,
		pq.Array(rows.ids), pq.Array(rows.types), pq.Array(rows.parents), pq.Array(rows.positions), rootID,
	)
	if err != nil {
		return err
	}

	// After the upsert, anything still hanging under the root but absent from the request was removed.
	_, err = tx.Exec(`
		WITH RECURSIVE subtree AS (
			SELECT id FROM nodes WHERE id = $1
			UNION ALL
			SELECT n.id FROM nodes n JOIN subtree s ON n.parent_id = s.id
		)
		DELETE FROM nodes
		WHERE id IN (SELECT id FROM subtree) AND id <> ALL($2)`,
		rootID, pq.Array(rows.ids),
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func postHierarchy(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var root nodeInput
		if err := json.NewDecoder(c.Request.Body).Decode(&root); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
			return
		}
		rows, err := flatten(&root)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		err = storeHierarchy(db, *root.ID, rows)
		switch {
		case errors.Is(err, errCycle):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		case err != nil:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusOK, gin.H{"id": *root.ID, "nodes": len(rows.ids)})
		}
	}
}

var errNotFound = errors.New("node not found")

func fetchHierarchy(db *sql.DB, rootID int64) (*Node, error) {
	// Ordering by depth guarantees a parent is built before its children.
	rows, err := db.Query(`
		WITH RECURSIVE subtree AS (
			SELECT id, type, parent_id, position, 0 AS depth FROM nodes WHERE id = $1
			UNION ALL
			SELECT n.id, n.type, n.parent_id, n.position, s.depth + 1
			FROM nodes n JOIN subtree s ON n.parent_id = s.id
		)
		SELECT id, type, parent_id FROM subtree ORDER BY depth, parent_id, position`,
		rootID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var root *Node
	byID := map[int64]*Node{}
	for rows.Next() {
		node := &Node{Children: []*Node{}}
		var parentID sql.NullInt64
		if err := rows.Scan(&node.ID, &node.Type, &parentID); err != nil {
			return nil, err
		}
		byID[node.ID] = node
		if root == nil {
			root = node
			continue
		}
		parent := byID[parentID.Int64]
		parent.Children = append(parent.Children, node)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if root == nil {
		return nil, errNotFound
	}
	return root, nil
}

func getHierarchy(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "node id must be an integer"})
			return
		}

		root, err := fetchHierarchy(db, id)
		switch {
		case errors.Is(err, errNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case err != nil:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusOK, root)
		}
	}
}
