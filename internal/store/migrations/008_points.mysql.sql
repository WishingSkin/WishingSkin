CREATE TABLE IF NOT EXISTS point_entries (
    entry_id CHAR(32) NOT NULL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    kind VARCHAR(16) NOT NULL,
    reference_id VARCHAR(64) NOT NULL,
    delta BIGINT NOT NULL,
    balance_after BIGINT NOT NULL,
    created_at BIGINT NOT NULL,
    UNIQUE (user_id,kind,reference_id),
    FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;

INSERT INTO settings(setting_key,value) VALUES('points_rules','{"enabled":false,"dailyReward":0,"uploadCost":0,"characterCost":0}')
ON DUPLICATE KEY UPDATE setting_key=VALUES(setting_key);
