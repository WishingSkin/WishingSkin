CREATE TABLE IF NOT EXISTS user_totp (
    user_id BIGINT NOT NULL PRIMARY KEY,
    secret_encrypted TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at BIGINT NOT NULL,
    last_used_step BIGINT NOT NULL DEFAULT -1,
    FOREIGN KEY(user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS user_totp_recovery (user_id BIGINT NOT NULL,code_hash CHAR(64) NOT NULL,PRIMARY KEY(user_id,code_hash),FOREIGN KEY(user_id) REFERENCES user_totp(user_id));
