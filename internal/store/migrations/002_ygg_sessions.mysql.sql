CREATE TABLE IF NOT EXISTS ygg_join_sessions (
    token_hash CHAR(64) NOT NULL PRIMARY KEY,
    server_id VARCHAR(255) NOT NULL,
    profile_uuid VARCHAR(32) NOT NULL,
    client_ip VARCHAR(45) NOT NULL,
    expires_at BIGINT NOT NULL,
    UNIQUE (server_id, profile_uuid),
    FOREIGN KEY (token_hash) REFERENCES game_tokens(token_hash) ON DELETE CASCADE,
    FOREIGN KEY (profile_uuid) REFERENCES characters(uuid) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
