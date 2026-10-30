package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"fmt"
	"time"
)

// LeaseStorage excludes uploads, physical cleanup and file snapshots across
// processes using the same database namespace. Other account writes may proceed.
func (s *Store) LeaseStorage(ctx context.Context) (func(), error) {
	return s.leaseOperation(ctx, "storage")
}

// LeasePluginTask excludes the same plugin task across application processes.
func (s *Store) LeasePluginTask(ctx context.Context, pluginID, taskID string) (func(), error) {
	return s.leaseOperation(ctx, "plugin-task:"+pluginID+":"+taskID)
}

func (s *Store) leaseOperation(ctx context.Context, scope string) (func(), error) {
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var namespace string
	query := "SELECT current_database() || ':' || current_schema()"
	if s.Dialect == "mysql" {
		query = "SELECT DATABASE()"
	}
	if err = conn.QueryRowContext(ctx, query).Scan(&namespace); err != nil {
		conn.Close()
		return nil, err
	}
	sum := sha256.Sum256([]byte("wishingskin:" + scope + ":" + namespace))
	name := fmt.Sprintf("ws-storage-%x", sum[:20])
	key := int64(binary.BigEndian.Uint64(sum[:8]))
	if s.Dialect == "mysql" {
		var acquired sql.NullInt64
		err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,5)", name).Scan(&acquired)
		if err == nil && (!acquired.Valid || acquired.Int64 != 1) {
			err = fmt.Errorf("storage is busy")
		}
	} else {
		lockCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err = conn.ExecContext(lockCtx, "SELECT pg_advisory_lock($1)", key)
		cancel()
	}
	if err != nil {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		conn.Close()
		return nil, err
	}
	return func() {
		defer conn.Close()
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var released bool
		if s.Dialect == "mysql" {
			var result sql.NullInt64
			err := conn.QueryRowContext(releaseCtx, "SELECT RELEASE_LOCK(?)", name).Scan(&result)
			released = err == nil && result.Valid && result.Int64 == 1
		} else {
			err := conn.QueryRowContext(releaseCtx, "SELECT pg_advisory_unlock($1)", key).Scan(&released)
			released = released && err == nil
		}
		if !released {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}, nil
}
