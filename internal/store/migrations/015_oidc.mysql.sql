CREATE TABLE IF NOT EXISTS oidc_identities (
    identity_key CHAR(64) NOT NULL PRIMARY KEY,
    issuer_key CHAR(64) NOT NULL,
    user_id BIGINT NOT NULL,
    issuer TEXT NOT NULL,
    subject VARCHAR(255) NOT NULL,
    created_at BIGINT NOT NULL,
    UNIQUE(user_id,issuer_key),
    FOREIGN KEY(user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
CREATE TABLE IF NOT EXISTS oidc_flows (
    flow_hash CHAR(64) NOT NULL PRIMARY KEY,
    value TEXT NOT NULL,
    expires_at BIGINT NOT NULL,
    INDEX oidc_flow_expiry(expires_at)
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
