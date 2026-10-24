CREATE TABLE IF NOT EXISTS point_entries (
    entry_id CHAR(32) COLLATE "C" NOT NULL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    kind VARCHAR(16) COLLATE "C" NOT NULL,
    reference_id VARCHAR(64) COLLATE "C" NOT NULL,
    delta BIGINT NOT NULL,
    balance_after BIGINT NOT NULL,
    created_at BIGINT NOT NULL,
    UNIQUE (user_id,kind,reference_id),
    FOREIGN KEY (user_id) REFERENCES users(id)
);

INSERT INTO settings(setting_key,value) VALUES('points_rules','{"enabled":false,"dailyReward":0,"uploadCost":0,"characterCost":0}')
ON CONFLICT (setting_key) DO NOTHING;
