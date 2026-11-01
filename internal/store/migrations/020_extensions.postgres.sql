CREATE TABLE IF NOT EXISTS plugin_data (plugin_id VARCHAR(64) NOT NULL, data_key VARCHAR(128) NOT NULL, value_json TEXT NOT NULL, PRIMARY KEY(plugin_id,data_key));
CREATE TABLE IF NOT EXISTS plugin_migrations (plugin_id VARCHAR(64) NOT NULL, version INTEGER NOT NULL, step INTEGER NOT NULL, checksum VARCHAR(64) NOT NULL, applied_at BIGINT NOT NULL, PRIMARY KEY(plugin_id,version,step));
CREATE TABLE IF NOT EXISTS webhook_subscriptions (id VARCHAR(64) PRIMARY KEY, plugin_id VARCHAR(64) NOT NULL, url TEXT NOT NULL, events_json TEXT NOT NULL, secret_json TEXT NOT NULL, enabled BOOLEAN NOT NULL);
CREATE TABLE IF NOT EXISTS webhook_deliveries (id VARCHAR(64) PRIMARY KEY, subscription_id VARCHAR(64) NOT NULL, payload_json TEXT NOT NULL, attempts INTEGER NOT NULL, next_at BIGINT NOT NULL, state VARCHAR(16) NOT NULL, status INTEGER NOT NULL, created_at BIGINT NOT NULL);
CREATE TABLE IF NOT EXISTS oauth_code_metadata (code_hash VARCHAR(64) PRIMARY KEY, nonce TEXT NOT NULL, auth_time BIGINT NOT NULL);
CREATE TABLE IF NOT EXISTS oauth_refresh_tokens (token_hash VARCHAR(64) PRIMARY KEY, family VARCHAR(64) NOT NULL, client_id VARCHAR(64) NOT NULL, user_id BIGINT NOT NULL, scope VARCHAR(256) NOT NULL, credential_hash VARCHAR(64) NOT NULL, expires_at BIGINT NOT NULL, used BOOLEAN NOT NULL);
CREATE TABLE IF NOT EXISTS plugin_event_deliveries (id VARCHAR(64) PRIMARY KEY, plugin_id VARCHAR(64) NOT NULL, payload_json TEXT NOT NULL, attempts INTEGER NOT NULL, next_at BIGINT NOT NULL, state VARCHAR(16) NOT NULL, created_at BIGINT NOT NULL);
CREATE INDEX IF NOT EXISTS webhook_due ON webhook_deliveries(state,next_at);
CREATE INDEX IF NOT EXISTS plugin_event_due ON plugin_event_deliveries(state,next_at);
