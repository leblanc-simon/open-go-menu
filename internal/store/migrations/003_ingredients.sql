-- The ingredients of the full dishes. They form their own catalogue: an
-- ingredient never becomes a starch, a protein or a vegetable.
CREATE TABLE ingredients (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT    NOT NULL
);

CREATE UNIQUE INDEX ingredients_name ON ingredients (name COLLATE NOCASE);

CREATE TABLE dish_ingredients (
    item_id       INTEGER NOT NULL REFERENCES items (id),
    ingredient_id INTEGER NOT NULL REFERENCES ingredients (id),
    position      INTEGER NOT NULL,
    PRIMARY KEY (item_id, ingredient_id)
);
