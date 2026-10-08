-- Full dishes become one kind of item among the components of a composed
-- meal: starch, protein (meat or fish), vegetable.
ALTER TABLE dishes RENAME TO items;
ALTER TABLE items ADD COLUMN kind TEXT NOT NULL DEFAULT 'dish';

-- Names are unique per list: "Rice" may be both a starch and a full dish.
DROP INDEX dishes_live_name;
CREATE UNIQUE INDEX items_live_name ON items (kind, name COLLATE NOCASE) WHERE deleted_at IS NULL;

-- A planned meal holds one row for a full dish, or one row per component.
CREATE TABLE plan_items (
    day     TEXT    NOT NULL,
    meal    TEXT    NOT NULL,
    kind    TEXT    NOT NULL,
    item_id INTEGER NOT NULL REFERENCES items (id),
    PRIMARY KEY (day, meal, kind)
);

INSERT INTO plan_items (day, meal, kind, item_id)
SELECT day, meal, 'dish', dish_id FROM plan_entries;

DROP TABLE plan_entries;
