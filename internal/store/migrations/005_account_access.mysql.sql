CREATE TABLE IF NOT EXISTS account_states (
    user_id BIGINT NOT NULL PRIMARY KEY,
    email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    verification_required BOOLEAN NOT NULL DEFAULT TRUE,
    FOREIGN KEY(user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
CREATE TABLE IF NOT EXISTS account_tokens (
    user_id BIGINT NOT NULL,
    purpose VARCHAR(8) NOT NULL,
    token_hash CHAR(64) NOT NULL UNIQUE,
    expires_at BIGINT NOT NULL,
    PRIMARY KEY(user_id,purpose),
    FOREIGN KEY(user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
CREATE TABLE IF NOT EXISTS invitation_codes (
    code_hash CHAR(64) NOT NULL PRIMARY KEY,
    expires_at BIGINT NOT NULL,
    remaining_uses INTEGER NOT NULL
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
INSERT INTO settings(setting_key,value) VALUES('registration_mode','open') ON DUPLICATE KEY UPDATE setting_key=setting_key;
