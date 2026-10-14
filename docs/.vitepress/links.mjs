export function rewriteDocLink(href) {
  const path = href.split(/[?#]/, 1)[0];
  if (path === "README.md" || path === "./README.md") {
    return `index.md${href.slice(path.length)}`;
  }
  if (
    path === "../README.md" ||
    path === "../AGENTS.md" ||
    path === "../deploy/porty.example.yaml"
  ) {
    return `https://github.com/msoldin/porty/blob/main/${path.slice(3)}${href.slice(path.length)}`;
  }
  return href;
}
