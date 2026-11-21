package store

import (
	"context"
	"database/sql"
)

// LeasePluginData serializes read-modify-write operations on a plugin's KV data.
// Use the leased connection for all queries performed while holding the lock.
func (s *Store) LeasePluginData(ctx context.Context, pluginID string) (*sql.Conn, func(), error) {
	return s.leaseOperation(ctx, "plugin-data:"+pluginID)
}
