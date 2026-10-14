import DefaultTheme from "vitepress/theme";
import { h } from "vue";
import logo from "../../branding/porty-head.png";
import "./style.css";

export default {
  extends: DefaultTheme,
  Layout: () =>
    h(DefaultTheme.Layout, null, {
      "nav-bar-title-before": () =>
        h("img", { src: logo, alt: "", class: "porty-logo" }),
      "doc-before": () =>
        h("aside", { class: "development-notice" }, [
          "Development documentation — main branch. ",
          h(
            "a",
            { href: "https://github.com/msoldin/porty/releases" },
            "Find version-matched guides in Releases.",
          ),
        ]),
    }),
};
