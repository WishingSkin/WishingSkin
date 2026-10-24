package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"
)

// RecordAdminEvent accepts only action names and resource identifiers, never request bodies or credentials.
// actorID 0 denotes the local operator who creates the first administrator.
func (s *Store) RecordAdminEvent(ctx context.Context, tx *sql.Tx, actorID int64, action, targetType, targetID string) error {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, s.Bind("INSERT INTO admin_events(event_id,actor_id,action,target_type,target_id,created_at) VALUES(?,?,?,?,?,?)"), hex.EncodeToString(id[:]), actorID, action, targetType, targetID, time.Now().Unix())
	return err
}
