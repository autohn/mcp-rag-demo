-- Records which demo projects were seeded, so a deleted demo project is not recreated.
CREATE TABLE seed_log (
    name       TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Databases seeded before this table existed already have the construction demo.
INSERT INTO seed_log(name)
SELECT 'construction' WHERE EXISTS (SELECT 1 FROM users WHERE email = 'admin@demo.local');
