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

type roleKey struct{}

// WithActorRole marks the request as acting in a role ("admin" or "manager"),
// recorded with every event it logs (PL-FR-15).
func WithActorRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, roleKey{}, role)
}

func actorRole(ctx context.Context) any {
	if r, _ := ctx.Value(roleKey{}).(string); r != "" {
		return r
	}
	return nil
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
	_, err := db.Exec(ctx, `INSERT INTO audit.events(actor_id, actor_role, action, target_type, target_id, details)
		VALUES ($1, $2, $3, $4, $5, $6)`, actor, actorRole(ctx), action, targetType, targetID, b)
	return err
}
