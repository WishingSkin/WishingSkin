CREATE TABLE IF NOT EXISTS account_states (
    user_id BIGINT NOT NULL PRIMARY KEY,
    email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    verification_required BOOLEAN NOT NULL DEFAULT TRUE,
    FOREIGN KEY(user_id) REFERENCES users(id)
);
CREATE TABLE IF NOT EXISTS account_tokens (
    user_id BIGINT NOT NULL,
    purpose VARCHAR(8) COLLATE "C" NOT NULL,
    token_hash CHAR(64) COLLATE "C" NOT NULL UNIQUE,
    expires_at BIGINT NOT NULL,
    PRIMARY KEY(user_id,purpose),
    FOREIGN KEY(user_id) REFERENCES users(id)
);
CREATE TABLE IF NOT EXISTS invitation_codes (
    code_hash CHAR(64) COLLATE "C" NOT NULL PRIMARY KEY,
    expires_at BIGINT NOT NULL,
    remaining_uses INTEGER NOT NULL
);
INSERT INTO settings(setting_key,value) VALUES('registration_mode','open') ON CONFLICT(setting_key) DO NOTHING;
