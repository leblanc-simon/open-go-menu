-- The planned meals, chosen per weekday (0 = Sunday) instead of for every
-- day at once. The former "meals" setting seeds every weekday.
CREATE TABLE weekly_meals (
    weekday INTEGER NOT NULL,
    meal    TEXT    NOT NULL,
    PRIMARY KEY (weekday, meal)
);

WITH days (weekday) AS (VALUES (0), (1), (2), (3), (4), (5), (6)),
     meals (meal) AS (VALUES ('lunch'), ('dinner'))
INSERT INTO weekly_meals (weekday, meal)
SELECT weekday, meal FROM days, meals
WHERE NOT EXISTS (SELECT 1 FROM settings WHERE key = 'meals')
   OR EXISTS (SELECT 1 FROM settings WHERE key = 'meals' AND ',' || value || ',' LIKE '%,' || meal || ',%');

DELETE FROM settings WHERE key = 'meals';

-- The plate always served on a meal of the week, laid out like plan_items.
CREATE TABLE fixed_items (
    weekday INTEGER NOT NULL,
    meal    TEXT    NOT NULL,
    kind    TEXT    NOT NULL,
    item_id INTEGER NOT NULL REFERENCES items (id),
    PRIMARY KEY (weekday, meal, kind)
);
