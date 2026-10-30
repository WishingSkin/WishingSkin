CREATE TABLE IF NOT EXISTS admin_event_changes (
    event_id CHAR(32) NOT NULL PRIMARY KEY,
    details_json MEDIUMTEXT NOT NULL,
    FOREIGN KEY (event_id) REFERENCES admin_events(event_id)
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
