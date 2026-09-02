CREATE TABLE project_skills (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id  UUID NOT NULL,
  name        TEXT NOT NULL,
  description TEXT NOT NULL,
  triggers    TEXT NULL,
  body        TEXT NOT NULL,
  created_by  UUID NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_project_skills_project_id_name ON project_skills(project_id, name);
