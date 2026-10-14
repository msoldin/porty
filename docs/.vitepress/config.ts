import { defineConfig } from "vitepress";
import { slug } from "github-slugger";
import { rewriteDocLink } from "./links.mjs";

export default defineConfig({
  title: "Porty",
  description:
    "Docker Compose, Git, and host monitoring for your Linux server.",
  base: "/porty/",
  cleanUrls: false,
  rewrites: { "README.md": "index.md" },
  srcExclude: [
    "superpowers/**",
    "design/**",
    "branding/**",
    "tests/**",
    "node_modules/**",
  ],
  markdown: {
    anchor: { slugify: slug },
    config(md) {
      md.core.ruler.after("inline", "porty-links", (state) => {
        for (const block of state.tokens) {
          for (const token of block.children || []) {
            if (token.type !== "link_open") continue;
            const href = token.attrGet("href");
            if (href) token.attrSet("href", rewriteDocLink(href));
          }
        }
      });
    },
  },
  themeConfig: {
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
    ],
    search: { provider: "local" },
    editLink: {
      pattern: "https://github.com/msoldin/porty/edit/main/docs/:path",
    },
  },
});
