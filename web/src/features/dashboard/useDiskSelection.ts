import { useState } from "preact/hooks";
import type { Device } from "./types";
type DiskChoice = { id: string; name: string };
const storageKey = "porty.dashboard.disks.v1";

function readSelection(): DiskChoice[] | undefined {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(storageKey) ?? "null");
    if (!Array.isArray(raw)) return undefined;
    const choices = raw
      .slice(0, 64)
      .filter(
        (item): item is DiskChoice =>
          item &&
          typeof item.id === "string" &&
          item.id.length > 0 &&
          item.id.length <= 128 &&
          typeof item.name === "string" &&
          item.name.length <= 256,
      );
    return raw.length > 0 && !choices.length ? undefined : choices;
  } catch {
    return undefined;
  }
}

export function useDiskSelection(devices: Device[]) {
  const [selection, setSelection] = useState<DiskChoice[] | undefined>(
    readSelection,
  );
  const [storageUnavailable, setStorageUnavailable] = useState(false);
  const filesystems = devices
    .filter((device) => device.kind === "filesystem")
    .sort(
      (a, b) =>
        Number(b.name === "/") - Number(a.name === "/") ||
        a.name.localeCompare(b.name),
    );
  const primary =
    filesystems.find((device) => device.default) ?? filesystems[0];
  const choices =
    selection ?? (primary ? [{ id: primary.id, name: primary.name }] : []);
  const ids = new Set(choices.map((choice) => choice.id));
  const visible = filesystems.filter((device) => ids.has(device.id));
  const missing = choices.filter(
    (choice) => !filesystems.some((device) => device.id === choice.id),
  );
  function choose(device: DiskChoice, checked: boolean) {
    const next = checked
      ? [
          ...choices.filter((choice) => choice.id !== device.id),
          { id: device.id, name: device.name },
        ]
      : choices.filter((choice) => choice.id !== device.id);
    setSelection(next);
    try {
      localStorage.setItem(storageKey, JSON.stringify(next));
      setStorageUnavailable(false);
    } catch {
      setStorageUnavailable(true);
    }
  }
  return { filesystems, visible, missing, ids, choose, storageUnavailable };
}
