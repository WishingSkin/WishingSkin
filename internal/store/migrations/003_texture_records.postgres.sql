CREATE TABLE IF NOT EXISTS texture_records (
    record_id CHAR(32) COLLATE "C" NOT NULL PRIMARY KEY,
    file_hash VARCHAR(128) COLLATE "C" NOT NULL,
    owner_id BIGINT NULL,
    name VARCHAR(255) COLLATE "C" NOT NULL,
    description TEXT COLLATE "C" NOT NULL,
    kind VARCHAR(8) COLLATE "C" NOT NULL,
    model VARCHAR(8) COLLATE "C" NOT NULL,
    is_public BOOLEAN NOT NULL DEFAULT FALSE,
    deleted BOOLEAN NOT NULL DEFAULT FALSE,
    FOREIGN KEY (file_hash) REFERENCES textures(hash),
    FOREIGN KEY (owner_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS texture_records_owner ON texture_records(owner_id);

INSERT INTO texture_records(record_id,file_hash,owner_id,name,description,kind,model,is_public,deleted)
SELECT LOWER(MD5(hash)),hash,NULL,name,'',kind,model,is_public,deleted FROM textures
ON CONFLICT (record_id) DO NOTHING;

INSERT INTO settings(setting_key,value) VALUES('character_limit','1')
ON CONFLICT (setting_key) DO NOTHING;
