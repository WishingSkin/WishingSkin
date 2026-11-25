ALTER TABLE oauth_clients ADD COLUMN allowed_scopes VARCHAR(512) NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS point_codes (
    code_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    encrypted_code TEXT NOT NULL,
    points BIGINT NOT NULL,
    max_uses INTEGER NOT NULL,
    remaining_uses INTEGER NOT NULL,
    expires_at BIGINT NOT NULL DEFAULT 0,
    note VARCHAR(48) NOT NULL DEFAULT '',
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at BIGINT NOT NULL,
    INDEX point_codes_created_idx(created_at, code_hash)
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS point_code_redemptions (
    code_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    user_id BIGINT NOT NULL,
    redeemed_at BIGINT NOT NULL,
    PRIMARY KEY (code_hash, user_id),
    FOREIGN KEY (code_hash) REFERENCES point_codes(code_hash) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
