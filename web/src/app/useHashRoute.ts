import { useEffect, useRef, useState } from "preact/hooks";

export function useHashRoute() {
  const [route, setRoute] = useState(location.hash.slice(1) || "/");
  const [dirty, setDirty] = useState(false);
  const dirtyRef = useRef(dirty);
  dirtyRef.current = dirty;
  const routeRef = useRef(route);
  routeRef.current = route;
  useEffect(() => {
    function hashChanged() {
      const next = location.hash.slice(1) || "/";
      if (next === routeRef.current) return;
      if (dirtyRef.current && !confirm("Discard unsaved changes?")) {
        history.replaceState(null, "", `#${routeRef.current}`);
        return;
      }
      setDirty(false);
      setRoute(next);
    }
    function beforeUnload(event: BeforeUnloadEvent) {
      if (dirtyRef.current) {
        event.preventDefault();
        event.returnValue = "";
      }
    }
    window.addEventListener("hashchange", hashChanged);
    window.addEventListener("beforeunload", beforeUnload);
    return () => {
      window.removeEventListener("hashchange", hashChanged);
      window.removeEventListener("beforeunload", beforeUnload);
    };
  }, []);
  function navigate(path: string) {
    if (dirty && !confirm("Discard unsaved changes?")) return;
    setDirty(false);
    setRoute(path);
    routeRef.current = path;
    location.hash = path;
  }
  return { route, navigate, dirty, setDirty };
}
