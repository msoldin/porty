import DefaultTheme from "vitepress/theme";
import { h } from "vue";
import { useData } from "vitepress";
import logo from "../../branding/porty-head.png";
import "./style.css";

export default {
  extends: DefaultTheme,
  Layout: () => {
    const { theme } = useData();
    const edition = theme.value.edition;
    return h(DefaultTheme.Layout, null, {
      "nav-bar-title-before": () =>
        h("img", { src: logo, alt: "", class: "porty-logo" }),
      "doc-before": () =>
        h("aside", { class: "development-notice" }, [
          edition.label + " ",
          h("a", { href: edition.switchHref }, edition.switchLabel),
        ]),
    });
  },
};
