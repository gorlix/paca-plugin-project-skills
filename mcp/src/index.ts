import {
  PluginAPIClient,
  type PluginMCPContext,
  type PluginMCPEntry,
  type Tool,
  errorResult,
  textResult,
} from "@paca-ai/plugin-sdk-mcp";

// ── Domain types ──────────────────────────────────────────────────────────────
// Mirrors backend/plugin.go's skillSummary/skillDetail JSON shape (and
// frontend/src/constants.ts's TS equivalent) — redeclared here since this is
// a separate Node package with no shared build against the frontend.

interface SkillSummary {
  name: string;
  description: string;
  triggers: string[] | null;
  doc_id: string;
  created_at: string;
  updated_at: string;
}

interface SkillDetail {
  name: string;
  description: string;
  triggers: string[] | null;
  doc_id: string;
  content: string;
}

// ── Formatting helpers ────────────────────────────────────────────────────────

function formatSummary(skill: SkillSummary): string {
  const triggers = skill.triggers?.length ? skill.triggers.join(", ") : "(none)";
  return `- ${skill.name}: ${skill.description} (triggers: ${triggers})`;
}

// ── Tool definitions ──────────────────────────────────────────────────────────

const PROJECT_ID_DESC =
  "The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID.";

const tools: Tool[] = [
  {
    name: "project_skills_list",
    description:
      "List the Agent Skills registered for a Paca project (agentskills.io format). Returns each skill's name, description, and trigger phrases — use project_skills_get to fetch a specific skill's full SKILL.md instructions.",
    inputSchema: {
      type: "object",
      properties: {
        projectId: { type: "string", description: PROJECT_ID_DESC },
      },
      required: ["projectId"],
    },
  },
  {
    name: "project_skills_get",
    description:
      "Get the full SKILL.md content (YAML frontmatter + markdown instructions) for one of a project's Agent Skills, by name. Use project_skills_list first to find the skill's name.",
    inputSchema: {
      type: "object",
      properties: {
        projectId: { type: "string", description: PROJECT_ID_DESC },
        name: {
          type: "string",
          description:
            "The skill's name, e.g. 'paca-database-review'. Use project_skills_list to get valid names.",
        },
      },
      required: ["projectId", "name"],
    },
  },
];

// ── Entry ─────────────────────────────────────────────────────────────────────

const entry: PluginMCPEntry = {
  tools,

  async handleToolCall(
    name: string,
    args: Record<string, unknown>,
    context: PluginMCPContext,
  ) {
    const api = new PluginAPIClient(context);

    try {
      switch (name) {
        case "project_skills_list": {
          const { projectId } = args as { projectId: string };
          const skills = await api.pluginGet<SkillSummary[]>(
            `projects/${projectId}/skills`,
          );
          if (skills.length === 0) {
            return textResult("No skills registered for this project.");
          }
          return textResult(
            `Project Skills:\n\n${skills.map(formatSummary).join("\n")}`,
          );
        }

        case "project_skills_get": {
          const { projectId, name: skillName } = args as {
            projectId: string;
            name: string;
          };
          const skill = await api.pluginGet<SkillDetail>(
            `projects/${projectId}/skills/${encodeURIComponent(skillName)}`,
          );
          return textResult(skill.content);
        }

        default:
          return errorResult(`Unknown tool: ${name}`);
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      return errorResult(`Tool ${name} failed: ${message}`);
    }
  },
};

export default entry;
