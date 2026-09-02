import react from "@vitejs/plugin-react";
import { federation } from "@module-federation/vite";
import { defineConfig } from "vite";

export default defineConfig({
  // Without this, Vite emits root-absolute asset URLs for anything loaded by
  // url() from CSS (fonts, images) that would resolve against the host
  // page's own origin/root instead of this plugin's deployed subpath. JS
  // chunk imports are unaffected — those resolve relative to remoteEntry.js's
  // own import.meta.url, not `base` — but any future CSS-referenced asset
  // needs this to land correctly.
  base: "/plugins/com.gorlix.project-skills/",
  plugins: [
    react(),
    federation({
      name: "com_gorlix_project_skills",
      filename: "remoteEntry.js",
      exposes: {
        "./ProjectSkillsPage": "./src/ProjectSkillsPage.tsx",
        "./ProjectSkillsSidebarSection": "./src/ProjectSkillsSidebarSection.tsx",
      },
      shared: {
        // Pinned exact (not ^19.0.0) — a patch-version mismatch against the
        // host's own React copy (e.g. our 19.2.8 vs its 19.2.5) made the
        // singleton negotiation fall back to bundling a second react-dom
        // instead of reusing the host's, which crashes on mount with React
        // error #527 ("multiple copies of react-dom"). Keep this in lockstep
        // with apps/web/package.json's react/react-dom version.
        react: { singleton: true, requiredVersion: "19.2.5" },
        "react-dom": { singleton: true, requiredVersion: "19.2.5" },
        "@tanstack/react-query": { singleton: true, requiredVersion: "^5.0.0" },
      },
    }),
  ],
  build: {
    target: "esnext",
    minify: false,
  },
});
