-- The shopping list. Ingredients become articles: anything that can be
-- bought, from an ingredient to toilet paper, each in an aisle of the shop.
CREATE TABLE aisles (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    name     TEXT    NOT NULL,
    position INTEGER NOT NULL
);

CREATE UNIQUE INDEX aisles_name ON aisles (name COLLATE NOCASE);

ALTER TABLE ingredients RENAME TO articles;
ALTER TABLE articles ADD COLUMN aisle_id INTEGER REFERENCES aisles (id);
ALTER TABLE dish_ingredients RENAME COLUMN ingredient_id TO article_id;
-- Free text: "200 g", "1 tin".
ALTER TABLE dish_ingredients ADD COLUMN quantity TEXT NOT NULL DEFAULT '';

-- The article bought for a starch, a protein or a vegetable. It survives a
-- renaming of the item, and so does its aisle.
CREATE TABLE component_articles (
    item_id    INTEGER PRIMARY KEY REFERENCES items (id),
    article_id INTEGER NOT NULL REFERENCES articles (id)
);

-- The current list: its days are in settings (shopping_from, shopping_to);
-- the articles added by hand and the ticked ones live until the next list.
CREATE TABLE shopping_extras (
    article_id INTEGER PRIMARY KEY REFERENCES articles (id),
    quantity   TEXT    NOT NULL
);

-- needs: what the article was bought for when ticked, so that a new need
-- unticks it.
CREATE TABLE shopping_checks (
    article_id INTEGER PRIMARY KEY REFERENCES articles (id),
    needs      TEXT    NOT NULL
);
