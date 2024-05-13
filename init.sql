CREATE TABLE users(
	email VARCHAR(320) NOT NULL PRIMARY KEY,
	name VARCHAR(64),
	prompt TEXT,
	sources TEXT,
	feed TEXT
);
