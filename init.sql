CREATE TABLE users(
	email VARCHAR(320) NOT NULL PRIMARY KEY,
	name VARCHAR(64),
	prompt TEXT,
	sources TEXT,
	feed TEXT,
	template TEXT,
	subscribe INTEGER
);

CREATE TABLE sessions(
	id VARCHAR(256),
	email VARCHAR(320),
	created_at TEXT DEFAULT CURRENT_TIMESTAMP
);
