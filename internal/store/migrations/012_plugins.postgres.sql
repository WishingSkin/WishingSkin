CREATE TABLE IF NOT EXISTS plugins (
    plugin_id VARCHAR(64) COLLATE "C" NOT NULL PRIMARY KEY,
    version VARCHAR(32) COLLATE "C" NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    settings_json TEXT COLLATE "C" NOT NULL
);

INSERT INTO plugins(plugin_id,version,enabled,settings_json) VALUES('wishingskin.points','1.0.0',TRUE,'{}')
ON CONFLICT (plugin_id) DO NOTHING;
INSERT INTO plugins(plugin_id,version,enabled,settings_json) VALUES('wishingskin.invitations','1.0.0',FALSE,'{}')
ON CONFLICT (plugin_id) DO NOTHING;
