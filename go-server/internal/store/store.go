// Package store persists hierarchies in Postgres.
package store

import (
	"context"
	"database/sql"

	"github.com/lib/pq"

	"go-server/internal/hierarchy"
)

// writeLockKey serializes writers; readers are never blocked.
const writeLockKey = 7340001

type Postgres struct {
	db *sql.DB
}

func NewPostgres(db *sql.DB) *Postgres {
	return &Postgres{db: db}
}

// Save makes the stored subtree under rows' root match rows exactly.
// The root keeps its current parent and position, if it already exists.
func (s *Postgres) Save(ctx context.Context, rows *hierarchy.FlatRows) error {
	rootID := rows.IDs[0]

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
		rootID, pq.Array(rows.IDs),
	).Scan(&cycle)
	if err != nil {
		return err
	}
	if cycle {
		return hierarchy.ErrCycle
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
		pq.Array(rows.IDs), pq.Array(rows.Types), pq.Array(rows.Parents), pq.Array(rows.Positions), rootID,
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
		rootID, pq.Array(rows.IDs),
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Postgres) Load(ctx context.Context, rootID int64) (*hierarchy.Node, error) {
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

	var flat []hierarchy.NodeRow
	for rows.Next() {
		var r hierarchy.NodeRow
		if err := rows.Scan(&r.ID, &r.Type, &r.ParentID); err != nil {
			return nil, err
		}
		flat = append(flat, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	root, err := hierarchy.BuildTree(flat)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, hierarchy.ErrNotFound
	}
	return root, nil
}
