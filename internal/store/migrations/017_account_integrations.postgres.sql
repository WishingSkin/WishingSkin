CREATE TABLE IF NOT EXISTS oauth_clients (
 client_id CHAR(32) NOT NULL PRIMARY KEY,
 name VARCHAR(128) NOT NULL,
 redirect_uris TEXT NOT NULL,
 secret_hash CHAR(64) NOT NULL,
 is_public BOOLEAN NOT NULL,
 enabled BOOLEAN NOT NULL
);
CREATE TABLE IF NOT EXISTS oauth_codes (
 code_hash CHAR(64) NOT NULL PRIMARY KEY,
 client_id CHAR(32) NOT NULL,
 user_id BIGINT NOT NULL,
 redirect_uri TEXT NOT NULL,
 scope VARCHAR(64) NOT NULL,
 challenge CHAR(43) NOT NULL,
 credential_hash TEXT NOT NULL,
 expires_at BIGINT NOT NULL,
 FOREIGN KEY(client_id) REFERENCES oauth_clients(client_id),
 FOREIGN KEY(user_id) REFERENCES users(id)
);
CREATE TABLE IF NOT EXISTS oauth_tokens (
 token_hash CHAR(64) NOT NULL PRIMARY KEY,
 client_id CHAR(32) NOT NULL,
 user_id BIGINT NOT NULL,
 scope VARCHAR(64) NOT NULL,
 credential_hash TEXT NOT NULL,
 expires_at BIGINT NOT NULL,
 FOREIGN KEY(client_id) REFERENCES oauth_clients(client_id),
 FOREIGN KEY(user_id) REFERENCES users(id)
);
CREATE TABLE IF NOT EXISTS external_identities (
 identity_key CHAR(64) NOT NULL PRIMARY KEY,
 connector_id VARCHAR(32) NOT NULL,
 subject VARCHAR(255) NOT NULL,
 username VARCHAR(255) NOT NULL,
 user_id BIGINT NOT NULL,
 created_at BIGINT NOT NULL,
 UNIQUE(user_id,connector_id),
 FOREIGN KEY(user_id) REFERENCES users(id)
);
