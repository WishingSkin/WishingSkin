CREATE TABLE IF NOT EXISTS plugins (
    plugin_id VARCHAR(64) NOT NULL PRIMARY KEY,
    version VARCHAR(32) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    settings_json TEXT NOT NULL
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;

INSERT INTO plugins(plugin_id,version,enabled,settings_json) VALUES('wishingskin.points','1.0.0',TRUE,'{}')
ON DUPLICATE KEY UPDATE plugin_id=VALUES(plugin_id);
INSERT INTO plugins(plugin_id,version,enabled,settings_json) VALUES('wishingskin.invitations','1.0.0',FALSE,'{}')
ON DUPLICATE KEY UPDATE plugin_id=VALUES(plugin_id);
