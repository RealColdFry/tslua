// @ts-check
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import react from "@astrojs/react";
import { visit } from "unist-util-visit";

/** Mark external links with ↗ indicator and rel="noopener". */
function rehypeExternalLinks() {
  return (tree) => {
    visit(tree, "element", (node) => {
      if (node.tagName !== "a") return;
      const href = node.properties?.href;
      if (typeof href !== "string" || !href.startsWith("http")) return;
      node.properties.rel = (node.properties.rel || []).concat("noopener");
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
          items: [
            { label: "tsconfig.json", slug: "config/tsconfig" },
            { label: "Class Styles", slug: "config/class-style" },
          ],
        },
      ],
    }),
  ],
});
