CREATE TABLE IF NOT EXISTS texture_file_integrity (
    file_hash VARCHAR(128) NOT NULL PRIMARY KEY,
    byte_size BIGINT NOT NULL CHECK (byte_size >= 0),
    sha256 CHAR(64) NOT NULL,
    FOREIGN KEY (file_hash) REFERENCES textures(hash) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
