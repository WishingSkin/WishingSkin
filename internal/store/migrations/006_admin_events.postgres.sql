CREATE TABLE IF NOT EXISTS admin_events (
    event_id CHAR(32) COLLATE "C" NOT NULL PRIMARY KEY,
    actor_id BIGINT NOT NULL,
    action VARCHAR(64) COLLATE "C" NOT NULL,
    target_type VARCHAR(32) COLLATE "C" NOT NULL,
    target_id VARCHAR(64) COLLATE "C" NOT NULL,
    created_at BIGINT NOT NULL
);
