import { defineConfig } from "vitepress";
import { slug } from "github-slugger";
import { rewriteDocLink } from "./links.mjs";
import { existsSync } from "node:fs";
import { resolve } from "node:path";

const source = process.env.PORTY_DOCS_SOURCE || process.cwd();
const ref = process.env.PORTY_DOCS_REF || "main";
const development = ref === "main";
const base = process.env.PORTY_DOCS_BASE || "/porty/";
const stableTag = process.env.PORTY_DOCS_STABLE_TAG || "";

export default defineConfig({
  title: "Porty",
  description:
    "Docker Compose, Git, and host monitoring for your Linux server.",
  base,
  srcDir: source,
  outDir: process.env.PORTY_DOCS_OUT || ".vitepress/dist",
  cleanUrls: false,
  rewrites: { "README.md": "index.md" },
  srcExclude: [
    "superpowers/**",
    "design/**",
    "branding/**",
    "tests/**",
    "node_modules/**",
    "scripts/**",
  ],
  markdown: {
    anchor: { slugify: slug },
    config(md) {
      md.core.ruler.after("inline", "porty-links", (state) => {
        for (const block of state.tokens) {
          for (const token of block.children || []) {
            if (token.type !== "link_open") continue;
            const href = token.attrGet("href");
            if (href) token.attrSet("href", rewriteDocLink(href, ref));
          }
        }
      });
    },
  },
  themeConfig: {
    edition: {
      development,
      label: development
        ? base === "/porty/" && !stableTag
          ? "No stable release yet — development documentation."
          : "Development documentation — main branch."
        : `Stable documentation — ${ref}.`,
      switchHref: base === "/porty/" ? "/porty/next/" : "/porty/",
      switchLabel:
        base === "/porty/"
          ? "View development docs"
          : stableTag
            ? `View stable docs (${stableTag})`
            : "Documentation home",
    },
    siteTitle: "Porty docs",
    nav: [
      { text: "GitHub", link: "https://github.com/msoldin/porty" },
      { text: "Releases", link: "https://github.com/msoldin/porty/releases" },
    ],
    sidebar: [
      {
        text: "Start here",
        items: [
          { text: "Overview", link: "/" },
          { text: "Getting started", link: "/getting-started" },
          { text: "Features", link: "/features" },
        ],
      },
      {
        text: "Use Porty",
        items: [
          { text: "User guide", link: "/user-guide" },
          { text: "Configuration", link: "/configuration" },
          { text: "Operator guide", link: "/operator-guide" },
          { text: "On-demand groups", link: "/on-demand" },
          { text: "Troubleshooting", link: "/troubleshooting" },
        ],
      },
      {
        text: "Contribute",
        items: [
          { text: "Security", link: "/security" },
          { text: "Development", link: "/development" },
          { text: "Releasing", link: "/releasing" },
        ],
      },
    ].map((section) => ({
      ...section,
      items: section.items.filter(
        (item) =>
          item.link === "/" ||
          existsSync(resolve(source, `${item.link.slice(1)}.md`)),
      ),
    })),
    search: { provider: "local" },
    editLink: {
      pattern: `https://github.com/msoldin/porty/${development ? "edit" : "blob"}/${ref}/docs/:path`,
      text: development ? "Edit this page" : `View source at ${ref}`,
    },
  },
});
