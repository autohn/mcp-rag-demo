-- Remember which project a seed created, so the "demo project" button can open it.
ALTER TABLE seed_log ADD COLUMN project_id BIGINT REFERENCES projects(id) ON DELETE SET NULL;

UPDATE seed_log SET project_id = (
    SELECT id FROM projects WHERE name = 'CV — Mikhail Aleksanian' ORDER BY id LIMIT 1
) WHERE name = 'cv';
