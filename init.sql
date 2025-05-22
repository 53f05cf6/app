CREATE TABLE users(
	username VARCHAR(32) NOT NULL PRIMARY KEY,
	email TEXT NOT NULL UNIQUE
);

CREATE TABLE user_sign_up_email_tokens(
	username VARCHAR(32) NOT NULL PRIMARY KEY,
	email TEXT NOT NULL UNIQUE,
	token TEXT NOT NULL,
	created_at TEXT DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE user_log_in_email_tokens(
	email TEXT NOT NULL,
	token TEXT NOT NULL,
	created_at TEXT DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE user_log_in_sessions(
	username REFERENCES users,
	id VARCHAR(256),
	created_at TEXT DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE bsky_feed_taiwanese_users(
	did TEXT NOT NULL PRIMARY KEY,
	created_at TEXT DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_created_at_desc_did ON bsky_feed_taiwanese_users(created_at DESC, did);

CREATE TABLE bsky_feed_taiwanese_block_users(
	did TEXT NOT NULL PRIMARY KEY
);

CREATE TABLE bsky_feed_taiwanese_posts(
	uri TEXT NOT NULL PRIMARY KEY,
	cid TEXT NOT NULL, 
	created_at TEXT NOT NULL
);

CREATE INDEX idx_created_at_desc_cid ON bsky_feed_taiwanese_posts(created_at DESC, cid);
