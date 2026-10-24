CREATE TABLE IF NOT EXISTS user_profiles (
    user_id BIGINT NOT NULL PRIMARY KEY,
    avatar_mode VARCHAR(16) COLLATE "C" NOT NULL,
    character_uuid VARCHAR(32) COLLATE "C" NOT NULL,
    avatar_image TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id)
);
