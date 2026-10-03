// Package audit records teacher, admin and system actions (NFR-15, ADR-16).
package audit

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgconn"
)

// Execer is satisfied by *pgxpool.Pool and pgx.Tx so audit rows can be
// written inside the transaction of the action they describe.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Log writes one audit event. actorID may be "" for system actions.
func Log(ctx context.Context, db Execer, actorID, action, targetType, targetID string, details any) error {
	var b []byte
	if details != nil {
		var err error
		if b, err = json.Marshal(details); err != nil {
			return err
		}
	} else {
		b = []byte("{}")
	}
	var actor any
	if actorID != "" {
		actor = actorID
	}
	_, err := db.Exec(ctx, `INSERT INTO audit.events(actor_id, action, target_type, target_id, details)
		VALUES ($1, $2, $3, $4, $5)`, actor, action, targetType, targetID, b)
	return err
}
