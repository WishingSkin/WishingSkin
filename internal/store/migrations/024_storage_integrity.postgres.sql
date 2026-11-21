CREATE TABLE IF NOT EXISTS texture_file_integrity (
    file_hash VARCHAR(128) COLLATE "C" NOT NULL PRIMARY KEY,
    byte_size BIGINT NOT NULL CHECK (byte_size >= 0),
    sha256 CHAR(64) COLLATE "C" NOT NULL,
    FOREIGN KEY (file_hash) REFERENCES textures(hash) ON DELETE CASCADE
);
