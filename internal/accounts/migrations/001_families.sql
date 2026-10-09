-- One family per authx user: the login is the users.email column (no e-mail
-- is ever asked for). The family's data lives in its own database file.
CREATE TABLE families (
    user_id        INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    file           TEXT    NOT NULL UNIQUE,
    created_at     TEXT    NOT NULL,
    last_active_at TEXT    NOT NULL
);

CREATE INDEX families_active ON families (last_active_at);

-- Single-use invitation codes, kept as their SHA-256 only.
CREATE TABLE invites (
    code_hash  TEXT PRIMARY KEY,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    used_at    TEXT
);
