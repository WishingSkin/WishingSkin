CREATE TABLE IF NOT EXISTS texture_records (
    record_id CHAR(32) NOT NULL PRIMARY KEY,
    file_hash VARCHAR(128) NOT NULL,
    owner_id BIGINT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    kind VARCHAR(8) NOT NULL,
    model VARCHAR(8) NOT NULL,
    is_public BOOLEAN NOT NULL DEFAULT FALSE,
    deleted BOOLEAN NOT NULL DEFAULT FALSE,
    KEY texture_records_owner(owner_id),
    FOREIGN KEY (file_hash) REFERENCES textures(hash),
    FOREIGN KEY (owner_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;

INSERT INTO texture_records(record_id,file_hash,owner_id,name,description,kind,model,is_public,deleted)
SELECT LOWER(MD5(hash)),hash,NULL,name,'',kind,model,is_public,deleted FROM textures
ON DUPLICATE KEY UPDATE record_id=VALUES(record_id);

INSERT INTO settings(setting_key,value) VALUES('character_limit','1')
ON DUPLICATE KEY UPDATE setting_key=VALUES(setting_key);
