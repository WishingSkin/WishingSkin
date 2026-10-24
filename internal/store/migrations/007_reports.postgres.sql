CREATE TABLE IF NOT EXISTS reports (
    report_id CHAR(32) COLLATE "C" NOT NULL PRIMARY KEY,
    reporter_id BIGINT NOT NULL,
    record_id CHAR(32) COLLATE "C" NOT NULL,
    texture_name VARCHAR(255) COLLATE "C" NOT NULL,
    reason TEXT COLLATE "C" NOT NULL,
    status VARCHAR(16) COLLATE "C" NOT NULL DEFAULT 'pending',
    resolution VARCHAR(16) COLLATE "C" NOT NULL DEFAULT '',
    reply TEXT COLLATE "C" NOT NULL,
    reviewer_id BIGINT NULL,
    created_at BIGINT NOT NULL,
    resolved_at BIGINT NOT NULL DEFAULT 0,
    UNIQUE (reporter_id,record_id),
    FOREIGN KEY (reporter_id) REFERENCES users(id),
    FOREIGN KEY (record_id) REFERENCES texture_records(record_id),
    FOREIGN KEY (reviewer_id) REFERENCES users(id)
);
