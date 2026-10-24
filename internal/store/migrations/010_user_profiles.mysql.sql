CREATE TABLE IF NOT EXISTS user_profiles (
    user_id BIGINT NOT NULL PRIMARY KEY,
    avatar_mode VARCHAR(16) NOT NULL,
    character_uuid VARCHAR(32) NOT NULL,
    avatar_image TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
