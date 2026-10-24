CREATE TABLE IF NOT EXISTS site_notices (
    notice_id VARCHAR(64) COLLATE "C" NOT NULL PRIMARY KEY,
    content TEXT NOT NULL,
    format VARCHAR(16) COLLATE "C" NOT NULL,
    created_at BIGINT NOT NULL
);
