-- Skill bodies are no longer authored/stored by this plugin directly — each
-- skill links to a real project Document instead, so editing the body reuses
-- the host's own Documentation editor rather than a second one built here.
ALTER TABLE project_skills DROP COLUMN body;
ALTER TABLE project_skills ADD COLUMN doc_id UUID;

CREATE INDEX idx_project_skills_doc_id ON project_skills(doc_id);
