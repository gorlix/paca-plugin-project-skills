import { useEffect, useRef, useState } from "react";
import type { CSSProperties } from "react";
import { PluginQueryClientProvider, usePluginQuery, usePluginQueryClient } from "@paca-ai/plugin-sdk-react";
import type { SidebarProjectSectionProps } from "@paca-ai/plugin-sdk-react";
import { ChevronRight, FileText, Folder, MoreHorizontal, Plus, Sparkles } from "lucide-react";
import { PLUGIN_ID, type SkillSummary } from "./constants";

const PAGE_PATH_SUFFIX = "plugins/com.gorlix.project-skills/skills";

export default function ProjectSkillsSidebarSection(props: SidebarProjectSectionProps) {
  return (
    <PluginQueryClientProvider>
      <Content {...props} />
    </PluginQueryClientProvider>
  );
}

function Content({ api, ui, projectId, isCollapsed }: SidebarProjectSectionProps) {
  const queryClient = usePluginQueryClient();
  const [sectionOpen, setSectionOpen] = useState(true);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [menuOpenFor, setMenuOpenFor] = useState<string | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);

  const skills = usePluginQuery(PLUGIN_ID, ["skills", projectId], () =>
    api.pluginGet<SkillSummary[]>(PLUGIN_ID, `/projects/${projectId}/skills`),
  );

  // ProjectSkillsPage (the project.page metadata form) is a separate
  // RemoteComponent mount with its own PluginQueryClientProvider — the host
  // doesn't share one queryClient between extension points, so a skill
  // created/edited there can't invalidate this component's cache directly.
  // It dispatches this event after every mutation instead; confirmed live
  // that without this, a skill created via the page didn't show up here
  // after an in-app navigation, only after a full page reload.
  useEffect(() => {
    function onSkillsChanged() {
      void queryClient.invalidateQueries({ queryKey: ["plugin", PLUGIN_ID, "skills", projectId] });
    }
    window.addEventListener("paca:project-skills-changed", onSkillsChanged);
    return () => window.removeEventListener("paca:project-skills-changed", onSkillsChanged);
  }, [queryClient, projectId]);

  useEffect(() => {
    if (!menuOpenFor) return;
    function onDocClick(e: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setMenuOpenFor(null);
      }
    }
    document.addEventListener("mousedown", onDocClick);
    return () => document.removeEventListener("mousedown", onDocClick);
  }, [menuOpenFor]);

  function toggleExpanded(name: string) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }

  function openSkill(name: string) {
    ui.navigate(`/projects/${projectId}/${PAGE_PATH_SUFFIX}?open=${encodeURIComponent(name)}`);
  }

  function newSkill() {
    ui.navigate(`/projects/${projectId}/${PAGE_PATH_SUFFIX}?new=1`);
  }

  async function remove(name: string) {
    const ok = await ui.confirm({
      title: `Delete "${name}"?`,
      description: "This cannot be undone.",
      variant: "destructive",
    });
    if (!ok) return;
    try {
      await api.pluginDelete(PLUGIN_ID, `/projects/${projectId}/skills/${name}`);
      ui.toast({ title: "Deleted", variant: "success" });
      await queryClient.invalidateQueries({ queryKey: ["plugin", PLUGIN_ID, "skills", projectId] });
      window.dispatchEvent(new Event("paca:project-skills-changed"));
    } catch (err) {
      ui.toast({ title: "Delete failed", description: String(err), variant: "destructive" });
    }
  }

  if (isCollapsed) {
    return (
      <div style={iconOnlyWrapStyle} title="Skills">
        <Sparkles size={16} />
      </div>
    );
  }

  return (
    <div style={groupStyle}>
      <button type="button" style={groupLabelStyle} onClick={() => setSectionOpen((v) => !v)}>
        <span>Skills</span>
        <ChevronRight size={13} style={chevronStyle(sectionOpen)} />
      </button>

      {sectionOpen && (
        <div style={{ padding: "4px 0" }}>
          {skills.isLoading && <div style={emptyStyle}>Loading…</div>}
          {skills.data?.length === 0 && <div style={emptyStyle}>No skills yet</div>}

          {skills.data?.map((skill) => {
            const isOpen = expanded.has(skill.name);
            return (
              <div key={skill.name}>
                <div style={rowStyle}>
                  <button type="button" style={rowButtonStyle} onClick={() => toggleExpanded(skill.name)}>
                    <ChevronRight size={12} style={{ ...chevronStyle(isOpen), flexShrink: 0 }} />
                    <Folder size={14} style={{ opacity: 0.6, flexShrink: 0 }} />
                    <span style={truncateStyle}>{skill.name}</span>
                  </button>
                  <div style={{ position: "relative" }} ref={menuOpenFor === skill.name ? menuRef : undefined}>
                    <button
                      type="button"
                      style={menuButtonStyle}
                      onClick={() => setMenuOpenFor((cur) => (cur === skill.name ? null : skill.name))}
                    >
                      <MoreHorizontal size={13} />
                    </button>
                    {menuOpenFor === skill.name && (
                      <div style={menuPopoverStyle}>
                        <button
                          type="button"
                          style={menuItemStyle}
                          onClick={() => {
                            setMenuOpenFor(null);
                            void remove(skill.name);
                          }}
                        >
                          Delete
                        </button>
                      </div>
                    )}
                  </div>
                </div>
                {isOpen && (
                  <button type="button" style={fileRowStyle} onClick={() => openSkill(skill.name)}>
                    <FileText size={13} style={{ opacity: 0.6, flexShrink: 0 }} />
                    <span style={truncateStyle}>SKILL.md</span>
                  </button>
                )}
              </div>
            );
          })}

          <button type="button" style={addRowStyle} onClick={newSkill}>
            <Plus size={12} style={{ flexShrink: 0 }} />
            <span>Add</span>
          </button>
        </div>
      )}
    </div>
  );
}

// ── Styles — mirrors the host's Documentation sidebar section (same
// --sidebar-* CSS custom properties the host defines globally, so light/dark
// theming matches automatically without duplicating its palette). ──────────

function chevronStyle(open: boolean): CSSProperties {
  return {
    transform: open ? "rotate(90deg)" : "none",
    transition: "transform 150ms",
    opacity: 0.4,
  };
}

const groupStyle: CSSProperties = {
  padding: "0 8px",
  color: "var(--sidebar-foreground)",
  fontSize: 13,
};

// Matches SidebarGroupLabel's own base classes (components/ui/sidebar.tsx)
// so this reads as one more native section, not a differently-cased plugin
// label: text-xs font-medium text-sidebar-foreground/70, no uppercase.
const groupLabelStyle: CSSProperties = {
  display: "flex",
  alignItems: "center",
  justifyContent: "space-between",
  width: "100%",
  height: 32,
  padding: "0 8px",
  fontSize: 12,
  fontWeight: 500,
  opacity: 0.7,
  background: "transparent",
  border: "none",
  cursor: "pointer",
  color: "inherit",
};

const rowStyle: CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: 2,
  borderRadius: 6,
};

const rowButtonStyle: CSSProperties = {
  flex: 1,
  minWidth: 0,
  display: "flex",
  alignItems: "center",
  gap: 6,
  padding: "5px 6px",
  fontSize: 13,
  background: "transparent",
  border: "none",
  textAlign: "left",
  cursor: "pointer",
  color: "inherit",
  borderRadius: 6,
};

const fileRowStyle: CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: 6,
  width: "100%",
  padding: "5px 6px 5px 28px",
  fontSize: 12,
  opacity: 0.85,
  background: "transparent",
  border: "none",
  textAlign: "left",
  cursor: "pointer",
  color: "inherit",
  borderRadius: 6,
};

const menuButtonStyle: CSSProperties = {
  flexShrink: 0,
  padding: "4px 6px",
  background: "transparent",
  border: "none",
  cursor: "pointer",
  opacity: 0.5,
  color: "inherit",
};

const menuPopoverStyle: CSSProperties = {
  position: "absolute",
  right: 0,
  top: "100%",
  zIndex: 20,
  background: "var(--sidebar)",
  border: "1px solid var(--sidebar-border)",
  borderRadius: 8,
  boxShadow: "0 4px 12px rgba(0,0,0,0.25)",
  minWidth: 120,
  padding: 4,
};

const menuItemStyle: CSSProperties = {
  display: "block",
  width: "100%",
  padding: "6px 8px",
  fontSize: 12,
  textAlign: "left",
  background: "transparent",
  border: "none",
  borderRadius: 6,
  cursor: "pointer",
  color: "#ef4444",
};

const addRowStyle: CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: 6,
  width: "100%",
  padding: "5px 6px",
  marginTop: 2,
  fontSize: 12,
  opacity: 0.4,
  background: "transparent",
  border: "none",
  textAlign: "left",
  cursor: "pointer",
  color: "inherit",
  borderRadius: 6,
};

const truncateStyle: CSSProperties = {
  overflow: "hidden",
  textOverflow: "ellipsis",
  whiteSpace: "nowrap",
};

const emptyStyle: CSSProperties = {
  padding: "4px 6px",
  fontSize: 12,
  opacity: 0.4,
  fontStyle: "italic",
};

const iconOnlyWrapStyle: CSSProperties = {
  display: "flex",
  justifyContent: "center",
  padding: "6px 0",
  color: "var(--sidebar-foreground)",
  opacity: 0.7,
};
