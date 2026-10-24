CREATE TABLE IF NOT EXISTS ygg_join_sessions (
    token_hash CHAR(64) COLLATE "C" NOT NULL PRIMARY KEY,
    server_id VARCHAR(255) COLLATE "C" NOT NULL,
    profile_uuid VARCHAR(32) COLLATE "C" NOT NULL,
    client_ip VARCHAR(45) COLLATE "C" NOT NULL,
    expires_at BIGINT NOT NULL,
    UNIQUE (server_id, profile_uuid),
    FOREIGN KEY (token_hash) REFERENCES game_tokens(token_hash) ON DELETE CASCADE,
    FOREIGN KEY (profile_uuid) REFERENCES characters(uuid) ON DELETE CASCADE
);
