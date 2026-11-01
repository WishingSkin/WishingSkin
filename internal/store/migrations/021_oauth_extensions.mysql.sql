CREATE TABLE IF NOT EXISTS oauth_access_families (token_hash VARCHAR(64) PRIMARY KEY, family VARCHAR(64) NOT NULL);
CREATE TABLE IF NOT EXISTS oauth_refresh_metadata (family VARCHAR(64) PRIMARY KEY, nonce TEXT NOT NULL, auth_time BIGINT NOT NULL);
CREATE TABLE IF NOT EXISTS web_session_auth (token_hash VARCHAR(64) PRIMARY KEY, auth_time BIGINT NOT NULL);
ALTER TABLE oauth_refresh_tokens MODIFY COLUMN credential_hash TEXT NOT NULL;
