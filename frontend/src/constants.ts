export const PLUGIN_ID = "com.gorlix.project-skills";

export interface SkillSummary {
  name: string;
  description: string;
  triggers: string[] | null;
  doc_id: string;
  created_at: string;
  updated_at: string;
}

export interface SkillFile {
  path: string;
  content: string;
  updated_at: string;
}

export interface SkillDetail {
  name: string;
  description: string;
  triggers: string[] | null;
  doc_id: string;
  content: string;
  files: SkillFile[] | null;
}
