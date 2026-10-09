package main

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lib/pq"
)

var (
	errCycle    = errors.New("the posted root's current ancestors cannot appear inside its subtree")
	errNotFound = errors.New("node not found")
)

// writeLockKey serializes writers; readers are never blocked.
const writeLockKey = 7340001

type pgStore struct {
	db *sql.DB
}

// Save makes the stored subtree under rows' root match rows exactly.
// The root keeps its current parent and position, if it already exists.
func (s *pgStore) Save(ctx context.Context, rows *flatRows) error {
	rootID := rows.ids[0]

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, writeLockKey); err != nil {
		return err
	}

	var cycle bool
	err = tx.QueryRowContext(ctx, `
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

	_, err = tx.ExecContext(ctx, `
		INSERT INTO nodes (id, type, parent_id, position)
		SELECT id, type, CASE WHEN id = $5 THEN NULL ELSE parent_id END, position
		FROM unnest($1::bigint[], $2::text[], $3::bigint[], $4::int[]) AS t(id, type, parent_id, position)
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
	_, err = tx.ExecContext(ctx, `
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

func (s *pgStore) Load(ctx context.Context, rootID int64) (*Node, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH RECURSIVE subtree AS (
			SELECT id, type, parent_id, position, 0 AS depth FROM nodes WHERE id = $1
			UNION ALL
			SELECT n.id, n.type, n.parent_id, n.position, s.depth + 1
			FROM nodes n JOIN subtree s ON n.parent_id = s.id
		)
		SELECT id, type, COALESCE(parent_id, 0) FROM subtree ORDER BY depth, parent_id, position`,
		rootID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var flat []nodeRow
	for rows.Next() {
		var r nodeRow
		if err := rows.Scan(&r.ID, &r.Type, &r.ParentID); err != nil {
			return nil, err
		}
		flat = append(flat, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	root, err := buildTree(flat)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, errNotFound
	}
	return root, nil
}
