CREATE TABLE IF NOT EXISTS oidc_identities (
    identity_key CHAR(64) COLLATE "C" NOT NULL PRIMARY KEY,
    issuer_key CHAR(64) COLLATE "C" NOT NULL,
    user_id BIGINT NOT NULL,
    issuer TEXT NOT NULL,
    subject VARCHAR(255) COLLATE "C" NOT NULL,
    created_at BIGINT NOT NULL,
    UNIQUE(user_id,issuer_key),
    FOREIGN KEY(user_id) REFERENCES users(id)
);
CREATE TABLE IF NOT EXISTS oidc_flows (
    flow_hash CHAR(64) COLLATE "C" NOT NULL PRIMARY KEY,
    value TEXT NOT NULL,
    expires_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS oidc_flow_expiry ON oidc_flows(expires_at);
