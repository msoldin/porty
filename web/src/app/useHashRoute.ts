import { useEffect, useRef, useState } from "preact/hooks";

export function useHashRoute() {
  const [route, setRoute] = useState(location.hash.slice(1) || "/");
  const [dirty, setDirty] = useState(false);
  const [pendingRoute, setPendingRoute] = useState<string>();
  const pending = useRef<string>();
  const dirtyRef = useRef(dirty);
  dirtyRef.current = dirty;
  const routeRef = useRef(route);
  routeRef.current = route;
  useEffect(() => {
    function hashChanged() {
      const next = location.hash.slice(1) || "/";
      if (next === routeRef.current) return;
      if (dirtyRef.current) {
        history.replaceState(null, "", `#${routeRef.current}`);
        pending.current = next;
        setPendingRoute(next);
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
    if (path === routeRef.current) return;
    if (dirtyRef.current) {
      pending.current = path;
      setPendingRoute(path);
      return;
    }
    setDirty(false);
    setRoute(path);
    routeRef.current = path;
    location.hash = path;
  }
  function cancelNavigation() {
    pending.current = undefined;
    setPendingRoute(undefined);
  }
  function confirmNavigation() {
    const next = pending.current;
    if (!next) return;
    dirtyRef.current = false;
    setDirty(false);
    cancelNavigation();
    navigate(next);
  }
  return {
    route,
    navigate,
    dirty,
    setDirty,
    pendingRoute,
    confirmNavigation,
    cancelNavigation,
  };
}
