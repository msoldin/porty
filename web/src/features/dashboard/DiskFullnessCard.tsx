import { useId, useRef, useState } from "preact/hooks";
import { Icon } from "../../components/Icon";
import { MetricCard } from "./MetricCard";
import { ReadingStatus } from "./ReadingStatus";
import { currentReading, formatMetric } from "./metricPresentation";
import type { Device, MetricKind, MonitoringState } from "./types";

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

export function DiskFullnessCard({
  state,
  loading,
  stale,
  now,
}: {
  state?: MonitoringState;
  loading: boolean;
  stale: boolean;
  now?: number;
}) {
  const [selection, setSelection] = useState<DiskChoice[] | undefined>(
    readSelection,
  );
  const [choosing, setChoosing] = useState(false);
  const [storageUnavailable, setStorageUnavailable] = useState(false);
  const toggle = useRef<HTMLButtonElement>(null);
  const pickerId = useId();
  const filesystems = (state?.inventory.devices ?? [])
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
  const read = (device: Device, metric: MetricKind) =>
    currentReading(state, device.id, metric, now, stale);
  const bytes = (device: Device, metric: MetricKind) =>
    formatMetric(read(device, metric)?.value, "bytes");
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
  function closePicker() {
    setChoosing(false);
    toggle.current?.focus();
  }
  return (
    <MetricCard
      label="Disk fullness"
      state={
        filesystems.length
          ? "available"
          : loading
            ? "collecting"
            : "unavailable"
      }
      detailLabel="Filesystem details"
      details={
        visible.length ? (
          visible.map((device) => {
            const total = read(device, "filesystem_total")?.value;
            const used = read(device, "filesystem_used")?.value;
            const available = read(device, "filesystem_available")?.value;
            const reserved =
              total != null && used != null && available != null
                ? total - used - available
                : 0;
            return (
              <div class="filesystem-detail" key={device.id}>
                <strong>{device.name}</strong>
                <p>
                  {device.driver} ·{" "}
                  {(device.mountPaths ?? [device.name]).join(" · ")}
                </p>
                <p>
                  {bytes(device, "filesystem_used")} used of{" "}
                  {bytes(device, "filesystem_total")}
                </p>
                <p>{bytes(device, "filesystem_available")} available</p>
                {reserved > 0 && (
                  <p class="muted">
                    {formatMetric(reserved, "bytes")} reserved; excluded from
                    used and available capacity.
                  </p>
                )}
              </div>
            );
          })
        ) : (
          <p>Choose disks above to see their details.</p>
        )
      }
    >
      <button
        ref={toggle}
        class="disk-picker-toggle"
        type="button"
        aria-label="Choose disks"
        aria-expanded={choosing}
        aria-controls={pickerId}
        onClick={() => setChoosing(!choosing)}
      >
        <span>
          Disks: {visible.length} of {filesystems.length}
        </span>
        <Icon name="Chevron" />
      </button>
      {choosing && (
        <div
          id={pickerId}
          class="disk-picker"
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              event.preventDefault();
              event.stopPropagation();
              closePicker();
            }
          }}
        >
          <fieldset>
            <legend>Visible disks</legend>
            <p>Choose the disks you want to keep an eye on.</p>
            <div class="disk-picker-options">
              {filesystems.map((device) => (
                <label key={device.id}>
                  <input
                    type="checkbox"
                    aria-label={device.name}
                    checked={ids.has(device.id)}
                    onChange={(event) =>
                      choose(device, event.currentTarget.checked)
                    }
                  />
                  <span>{device.name}</span>
                  <small>
                    {formatMetric(
                      read(device, "filesystem_percent")?.value,
                      "percent",
                    )}
                  </small>
                </label>
              ))}
              {missing.map((device) => (
                <label key={device.id}>
                  <input
                    type="checkbox"
                    aria-label={device.name}
                    checked
                    onChange={() => choose(device, false)}
                  />
                  <span>{device.name}</span>
                  <small>Unavailable</small>
                </label>
              ))}
              {!filesystems.length && !missing.length && (
                <p>No disks available yet.</p>
              )}
            </div>
          </fieldset>
          <button type="button" onClick={closePicker}>
            Done
          </button>
        </div>
      )}
      {!loading && state && missing.length > 0 && (
        <p class="disk-selection-note" role="status">
          {missing.length === 1
            ? "A selected disk is unavailable."
            : missing.length + " selected disks are unavailable."}{" "}
          Choose disks to update your selection.
        </p>
      )}
      {storageUnavailable && (
        <p class="disk-selection-note">
          Your choices apply for this visit; browser storage is unavailable.
        </p>
      )}
      {!visible.length && filesystems.length > 0 && (
        <p class="disk-selection-note">
          {choices.length
            ? "No selected disks are currently available."
            : "No disks selected. Choose disks to show here."}
        </p>
      )}
      {visible.map((device) => {
        const percent = read(device, "filesystem_percent");
        return (
          <div class="capacity-row" key={device.id} data-state={percent?.state}>
            <div>
              <strong>{device.name}</strong>
              <span>{formatMetric(percent?.value, "percent")}</span>
            </div>
            <div
              class="capacity-bar"
              role="progressbar"
              aria-label={device.name + " disk fullness"}
              aria-valuenow={percent?.value ?? undefined}
              aria-valuemin={0}
              aria-valuemax={100}
            >
              <span style={{ width: (percent?.value ?? 0) + "%" }} />
            </div>
            <small>
              {bytes(device, "filesystem_available")} free of{" "}
              {bytes(device, "filesystem_total")}
            </small>
            <ReadingStatus
              state={percent?.state ?? "unavailable"}
              lastSuccessAt={percent?.lastSuccessAt}
            />
          </div>
        );
      })}
    </MetricCard>
  );
}
