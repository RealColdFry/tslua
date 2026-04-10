// @ts-check
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import react from "@astrojs/react";
import { visit } from "unist-util-visit";

/** Mark external links with ↗ indicator and rel="noopener". */
function rehypeExternalLinks() {
  return (/** @type {import("hast").Root} */ tree) => {
    visit(tree, "element", (node) => {
      if (node.tagName !== "a") return;
      const href = node.properties?.href;
      if (typeof href !== "string" || !href.startsWith("http")) return;
      const existing = node.properties.rel;
      const relList = Array.isArray(existing)
        ? existing.map(String)
        : existing
          ? [String(existing)]
          : [];
      node.properties.rel = [...relList, "noopener"];
      node.children.push({ type: "text", value: "\u00a0↗" });
    });
  };
}

// https://astro.build/config
export default defineConfig({
  site: "https://realcoldfry.github.io",
  base: "/tslua",
  markdown: {
    rehypePlugins: [rehypeExternalLinks],
  },
  integrations: [
    react(),
    starlight({
      title: "tslua",
      social: [{ icon: "github", label: "GitHub", href: "https://github.com/RealColdFry/tslua" }],
      components: {
        SiteTitle: "./src/components/starlight/SiteTitle.astro",
      },
      sidebar: [
        {
          label: "Getting Started",
          items: [
            { label: "Installation", slug: "guides/installation" },
            { label: "CLI Reference", slug: "cli/overview" },
          ],
        },
        {
          label: "Configuration",
          items: [{ label: "tsconfig.json", slug: "config/tsconfig" }],
        },
        {
          label: "Customization",
          items: [
            { label: "Class Styles", slug: "config/class-style" },
            { label: "Emit Mode", slug: "config/emit-mode" },
            { label: "Export as Global", slug: "config/export-as-global" },
          ],
        },
        {
          label: "Development",
          items: [
            { label: "Testing", slug: "testing/overview" },
            { label: "Performance", slug: "performance" },
            { label: "Background", slug: "background" },
          ],
        },
      ],
    }),
  ],
});
