-- Codes made by a family (EF-34): the family that made it, and the one that
-- signed up with it. Both NULL for the administrator's codes. The codes of a
-- deleted family go with it; a family it invited stays.
ALTER TABLE invites ADD COLUMN created_by INTEGER REFERENCES users (id) ON DELETE CASCADE;
ALTER TABLE invites ADD COLUMN used_by INTEGER REFERENCES users (id) ON DELETE SET NULL;

CREATE INDEX invites_created_by ON invites (created_by);
CREATE INDEX invites_used_by ON invites (used_by);
