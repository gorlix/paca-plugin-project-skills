import { useEffect, useState, useSyncExternalStore } from "react";
import { Check, ChevronRight, ExternalLink } from "lucide-react";
import {
  PluginQueryClientProvider,
  usePluginQueryClient,
} from "@paca-ai/plugin-sdk-react";
import type { ProjectPageProps } from "@paca-ai/plugin-sdk-react";
import { PLUGIN_ID, type SkillDetail } from "./constants";

export default function ProjectSkillsPage(props: ProjectPageProps) {
  return (
    <PluginQueryClientProvider>
      <Content {...props} />
    </PluginQueryClientProvider>
  );
}

type Selection = { kind: "none" } | { kind: "draft" } | { kind: "existing"; name: string };
type SaveState = "idle" | "saving" | "saved" | "error";

/** Patches history.pushState/replaceState (once, globally) to also dispatch
 * a plain event, since TanStack Router's client-side navigation doesn't fire
 * "popstate" the way back/forward does. Needed because this page is a single
 * `project.page` route reused across every skill — the sidebar navigates
 * between skills via `?open=<name>` on the *same* route match, which mounts
 * this component once and only changes the query string afterwards. */
function patchHistoryOnce() {
  const w = window as unknown as { __pacaHistoryPatched?: boolean };
  if (w.__pacaHistoryPatched) return;
  w.__pacaHistoryPatched = true;
  for (const key of ["pushState", "replaceState"] as const) {
    const original = history[key];
    history[key] = function (this: History, ...args: Parameters<History["pushState"]>) {
      const result = original.apply(this, args);
      window.dispatchEvent(new Event("paca:locationchange"));
      return result;
    };
  }
}

function useLocationSearch(): string {
  useEffect(() => {
    patchHistoryOnce();
  }, []);
  return useSyncExternalStore(
    (callback) => {
      window.addEventListener("popstate", callback);
      window.addEventListener("paca:locationchange", callback);
      return () => {
        window.removeEventListener("popstate", callback);
        window.removeEventListener("paca:locationchange", callback);
      };
    },
    () => window.location.search,
    () => "",
  );
}

function Content({ api, ui, projectId }: ProjectPageProps) {
  const queryClient = usePluginQueryClient();
  const search = useLocationSearch();

  const [selection, setSelection] = useState<Selection>({ kind: "none" });
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [triggersText, setTriggersText] = useState("");
  const [docId, setDocId] = useState<string | null>(null);
  const [saveState, setSaveState] = useState<SaveState>("idle");

  async function invalidateList() {
    await queryClient.invalidateQueries({ queryKey: ["plugin", PLUGIN_ID, "skills", projectId] });
  }

  function startNewDraft() {
    setSelection({ kind: "draft" });
    setName("");
    setDescription("");
    setTriggersText("");
    setDocId(null);
    setSaveState("idle");
  }

  async function openSkill(skillName: string) {
    try {
      const detail = await api.pluginGet<SkillDetail>(
        PLUGIN_ID,
        `/projects/${projectId}/skills/${skillName}`,
      );
      setSelection({ kind: "existing", name: skillName });
      setName(detail.name);
      setDescription(detail.description);
      setTriggersText((detail.triggers ?? []).join(", "));
      setDocId(detail.doc_id);
      setSaveState("idle");
    } catch (err) {
      ui.toast({ title: "Failed to open skill", description: String(err), variant: "destructive" });
    }
  }

  useEffect(() => {
    const params = new URLSearchParams(search);
    const open = params.get("open");
    if (open) {
      void openSkill(open);
    } else if (params.get("new") === "1") {
      startNewDraft();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [search]);

  function openDocument() {
    if (!docId) return;
    ui.navigate(`/projects/${projectId}/docs/${docId}`);
  }

  /** Finds this project's root-level "Skills" doc folder, creating it once
   * if missing. Every skill's linked document is filed under it — otherwise
   * it shows up as a bare, unlabeled entry in the Documentation list
   * indistinguishable from an ordinary doc. */
  async function ensureSkillsFolder(): Promise<string> {
    const listRes = await fetch(`/api/v1/projects/${projectId}/docs/folders`, {
      credentials: "include",
    });
    if (!listRes.ok) {
      throw new Error(`Failed to list doc folders (${listRes.status})`);
    }
    const listEnvelope = (await listRes.json()) as {
      data: { items: { id: string; name: string; parent_id: string | null }[] };
    };
    const existing = listEnvelope.data.items.find(
      (f) => f.name === "Skills" && !f.parent_id,
    );
    if (existing) return existing.id;

    const createRes = await fetch(`/api/v1/projects/${projectId}/docs/folders`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: "Skills" }),
    });
    if (!createRes.ok) {
      throw new Error(`Failed to create Skills folder (${createRes.status})`);
    }
    const createEnvelope = (await createRes.json()) as { data: { id: string } };
    return createEnvelope.data.id;
  }

  async function createSkill() {
    if (!name.trim()) {
      ui.toast({ title: "name is required", variant: "destructive" });
      return;
    }
    if (!description.trim()) {
      ui.toast({ title: "description is required", variant: "destructive" });
      return;
    }
    const triggers = triggersText
      .split(",")
      .map((t) => t.trim())
      .filter(Boolean);

    setSaveState("saving");
    try {
      // A skill's body is a real project Document — created here through the
      // host's own Documentation API (the same one its UI uses) rather than
      // reimplemented by this plugin, so it opens straight into the host's
      // real editor with nothing of our own in between. Filed under a
      // "Skills" folder so it reads as plugin-owned at a glance instead of
      // sitting indistinguishable among ordinary docs.
      const skillsFolderId = await ensureSkillsFolder();
      const docRes = await fetch(`/api/v1/projects/${projectId}/docs`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ title: name.trim(), folder_id: skillsFolderId }),
      });
      if (!docRes.ok) {
        throw new Error(`Failed to create document (${docRes.status})`);
      }
      const docEnvelope = (await docRes.json()) as { data: { id: string } };
      const newDocId = docEnvelope.data.id;

      const created = await api.pluginPost<SkillDetail>(
        PLUGIN_ID,
        `/projects/${projectId}/skills`,
        { name, description, triggers: triggers.length > 0 ? triggers : null, doc_id: newDocId },
      );
      ui.toast({ title: `Created ${created.name}`, variant: "success" });
      await invalidateList();
      ui.navigate(`/projects/${projectId}/docs/${newDocId}`);
    } catch (err) {
      ui.toast({ title: "Create failed", description: String(err), variant: "destructive" });
      setSaveState("error");
    }
  }

  async function saveMetadata() {
    if (selection.kind !== "existing") return;
    if (!description.trim()) {
      ui.toast({ title: "description is required", variant: "destructive" });
      return;
    }
    const triggers = triggersText
      .split(",")
      .map((t) => t.trim())
      .filter(Boolean);

    setSaveState("saving");
    try {
      const updated = await api.pluginPatch<SkillDetail>(
        PLUGIN_ID,
        `/projects/${projectId}/skills/${selection.name}`,
        { description, triggers: triggers.length > 0 ? triggers : null },
      );
      setDescription(updated.description);
      setTriggersText((updated.triggers ?? []).join(", "));
      setSaveState("saved");
      await invalidateList();
    } catch (err) {
      ui.toast({ title: "Save failed", description: String(err), variant: "destructive" });
      setSaveState("error");
    }
  }

  const busy = saveState === "saving";

  return (
    <div className="flex flex-col h-full min-h-0">
      {/* ── Header bar ───────────────────────────────────────────────── */}
      <div className="flex items-center justify-between px-4 py-2 bg-muted/20 border-b border-border/30 shrink-0 gap-3 min-w-0">
        <div className="flex items-center gap-1 min-w-0 text-xs">
          <span className="text-muted-foreground/50 truncate max-w-32">Skills</span>
          {selection.kind !== "none" && (
            <>
              <ChevronRight className="size-3.5 text-muted-foreground/30 shrink-0" />
              <span className="text-foreground/70 font-medium truncate max-w-60">
                {selection.kind === "draft" ? "New skill" : selection.name}
              </span>
            </>
          )}
        </div>

        <div className="flex items-center gap-2 shrink-0">
          {selection.kind === "existing" && saveState === "saved" && (
            <span className="text-xs text-muted-foreground/60 flex items-center gap-1">
              <Check className="size-3 text-emerald-500" />
              Saved
            </span>
          )}
          {selection.kind === "existing" && (
            <button
              type="button"
              disabled={busy}
              onClick={() => void saveMetadata()}
              className="group/button inline-flex shrink-0 items-center justify-center rounded-lg border border-transparent bg-clip-padding text-sm font-medium whitespace-nowrap transition-all outline-none select-none hover:bg-muted hover:text-foreground disabled:pointer-events-none disabled:opacity-50 h-7 gap-1 rounded-[min(var(--radius-md),12px)] px-2.5 text-xs"
            >
              Save
            </button>
          )}
          {selection.kind === "draft" && (
            <button
              type="button"
              disabled={busy}
              onClick={() => void createSkill()}
              className="group/button inline-flex shrink-0 items-center justify-center rounded-lg border border-transparent bg-clip-padding text-sm font-medium whitespace-nowrap transition-all outline-none select-none disabled:pointer-events-none disabled:opacity-50 bg-primary text-primary-foreground hover:bg-primary/85 h-7 gap-1 rounded-[min(var(--radius-md),12px)] px-2.5 text-xs"
            >
              Create
            </button>
          )}
        </div>
      </div>

      {/* ── Body ─────────────────────────────────────────────────────── */}
      <div className="flex flex-1 min-h-0 overflow-hidden">
        <div className="flex-1 overflow-y-auto [scrollbar-gutter:stable]">
          <div className="max-w-2xl mx-auto px-8 py-7">
            {selection.kind === "none" && (
              <div className="flex flex-col items-center justify-center gap-2 text-muted-foreground py-24">
                <p className="text-sm text-muted-foreground/70">
                  Select a skill from the sidebar, or add a new one.
                </p>
              </div>
            )}

            {selection.kind !== "none" && (
              <div className="grid gap-4">
                <label className="grid gap-1 text-xs font-medium text-muted-foreground/80">
                  Name
                  <input
                    value={name}
                    disabled={selection.kind === "existing"}
                    placeholder="my-new-skill"
                    onChange={(e) => setName(e.target.value)}
                    className="h-8 w-full min-w-0 rounded-lg border border-input bg-transparent px-2.5 py-1 text-base transition-colors outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:cursor-not-allowed disabled:bg-input/50 disabled:opacity-50 md:text-sm dark:bg-input/30 dark:disabled:bg-input/80 font-normal text-foreground"
                  />
                </label>
                <label className="grid gap-1 text-xs font-medium text-muted-foreground/80">
                  Description
                  <input
                    value={description}
                    placeholder="What this skill does and when to use it."
                    onChange={(e) => setDescription(e.target.value)}
                    className="h-8 w-full min-w-0 rounded-lg border border-input bg-transparent px-2.5 py-1 text-base transition-colors outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 md:text-sm dark:bg-input/30 font-normal text-foreground"
                  />
                </label>
                <label className="grid gap-1 text-xs font-medium text-muted-foreground/80">
                  Triggers <span className="font-normal text-muted-foreground/50">(comma-separated, optional)</span>
                  <input
                    value={triggersText}
                    placeholder="review this migration, check this schema change"
                    onChange={(e) => setTriggersText(e.target.value)}
                    className="h-8 w-full min-w-0 rounded-lg border border-input bg-transparent px-2.5 py-1 text-base transition-colors outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 md:text-sm dark:bg-input/30 font-normal text-foreground"
                  />
                </label>

                {selection.kind === "existing" && (
                  <button
                    type="button"
                    onClick={openDocument}
                    className="mt-2 group/button inline-flex items-center justify-center gap-1.5 rounded-lg border border-border bg-background text-sm font-medium transition-all outline-none select-none hover:bg-muted hover:text-foreground dark:border-input dark:bg-input/30 dark:hover:bg-input/50 h-8 px-3 w-fit"
                  >
                    <ExternalLink className="size-3.5" />
                    Open SKILL.md
                  </button>
                )}
                {selection.kind === "draft" && (
                  <p className="text-xs text-muted-foreground/60 -mt-1">
                    Creating this skill opens a new document — write the skill's
                    instructions there, in the same editor as every other project document.
                  </p>
                )}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
