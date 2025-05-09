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

CREATE TABLE user_log_in_sessions(
	username REFERENCES users,
	id VARCHAR(256),
	created_at TEXT DEFAULT CURRENT_TIMESTAMP
);
