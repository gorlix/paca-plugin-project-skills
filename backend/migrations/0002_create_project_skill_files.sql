CREATE TABLE project_skill_files (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  skill_id   UUID NOT NULL REFERENCES project_skills(id) ON DELETE CASCADE,
  path       TEXT NOT NULL,
  content    TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_project_skill_files_skill_id_path ON project_skill_files(skill_id, path);
