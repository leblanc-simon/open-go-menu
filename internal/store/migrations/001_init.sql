CREATE TABLE dishes (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT    NOT NULL,
    weight        INTEGER NOT NULL DEFAULT 5,
    weight_spring INTEGER NOT NULL DEFAULT 5,
    weight_summer INTEGER NOT NULL DEFAULT 5,
    weight_autumn INTEGER NOT NULL DEFAULT 5,
    weight_winter INTEGER NOT NULL DEFAULT 5,
    created_at    TEXT    NOT NULL,
    deleted_at    TEXT
);

-- Names are unique among live dishes only: a deleted dish frees its name.
CREATE UNIQUE INDEX dishes_live_name ON dishes (name COLLATE NOCASE) WHERE deleted_at IS NULL;

CREATE TABLE plan_entries (
    day     TEXT    NOT NULL,
    meal    TEXT    NOT NULL,
    dish_id INTEGER NOT NULL REFERENCES dishes (id),
    PRIMARY KEY (day, meal)
);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
