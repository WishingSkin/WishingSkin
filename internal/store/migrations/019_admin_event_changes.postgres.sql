CREATE TABLE IF NOT EXISTS admin_event_changes (
    event_id CHAR(32) COLLATE "C" NOT NULL PRIMARY KEY,
    details_json TEXT NOT NULL,
    FOREIGN KEY (event_id) REFERENCES admin_events(event_id)
);
