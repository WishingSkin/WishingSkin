package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"time"
)

type AdminChange struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}
type adminChangesKey struct{}

func WithAdminChanges(ctx context.Context) context.Context {
	return context.WithValue(ctx, adminChangesKey{}, &[]AdminChange{})
}

func AdminChangesEnabled(ctx context.Context) bool { return ctx.Value(adminChangesKey{}) != nil }

// CaptureAdminChange accepts only explicitly selected, display-safe values.
func CaptureAdminChange(ctx context.Context, field, before, after string) {
	if changes, ok := ctx.Value(adminChangesKey{}).(*[]AdminChange); ok {
		*changes = append(*changes, AdminChange{field, before, after})
	}
}

// RecordAdminEvent stores display-safe field changes, never raw request bodies or credentials.
// actorID 0 denotes the local operator who creates the first administrator.
func (s *Store) RecordAdminEvent(ctx context.Context, tx *sql.Tx, actorID int64, action, targetType, targetID string, changes ...AdminChange) error {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	eventID := hex.EncodeToString(id[:])
	if _, err := tx.ExecContext(ctx, s.Bind("INSERT INTO admin_events(event_id,actor_id,action,target_type,target_id,created_at) VALUES(?,?,?,?,?,?)"), eventID, actorID, action, targetType, targetID, time.Now().Unix()); err != nil {
		return err
	}
	if captured, ok := ctx.Value(adminChangesKey{}).(*[]AdminChange); ok {
		changes = append(changes, (*captured)...)
		*captured = nil
	}
	if len(changes) == 0 {
		return nil
	}
	data, err := json.Marshal(changes)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, s.Bind("INSERT INTO admin_event_changes(event_id,details_json) VALUES(?,?)"), eventID, string(data))
	return err
}
