-- Seasons stop replacing the general weight and add to it instead: a
-- seasonal weight from 0 to 10, multiplied by the general one, becomes a
-- bonus from -10 to +10 added to it (RG-01). The former neutral 5 becomes 0,
-- the scale keeps its steps, and 0 — never during the season — becomes -10,
-- which still keeps the item out whatever its general weight.
-- The columns keep their former DEFAULT 5: every insert sets them.
UPDATE items SET
    weight_spring = CASE weight_spring WHEN 0 THEN -10 ELSE weight_spring - 5 END,
    weight_summer = CASE weight_summer WHEN 0 THEN -10 ELSE weight_summer - 5 END,
    weight_autumn = CASE weight_autumn WHEN 0 THEN -10 ELSE weight_autumn - 5 END,
    weight_winter = CASE weight_winter WHEN 0 THEN -10 ELSE weight_winter - 5 END;
