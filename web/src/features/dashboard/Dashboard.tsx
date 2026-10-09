import { useState } from "preact/hooks";
import { useMonitoring } from "./useMonitoring";
import { MetricAvailability } from "./MetricAvailability";
import { MetricCard } from "./MetricCard";
import { MetricChart } from "./MetricChart";
import { ReadingStatus } from "./ReadingStatus";
import { Dialog } from "../../components/Dialog";
import { DiskPicker } from "./DiskPicker";
import { useDiskSelection } from "./useDiskSelection";
import { DiskFullnessCard } from "./DiskFullnessCard";
import {
  chartSeries,
  currentReading,
  formatMetric,
  selectDevice,
  uptime,
} from "./metricPresentation";
import type { Device, MetricKind, MonitoringState, State, Unit } from "./types";
import "./dashboard.css";

type Choices = { network?: string; temperature?: string; gpu?: string };
const storageKey = "porty.dashboard.v1";
function readChoices(): Choices {
  try {
    const value = JSON.parse(
      localStorage.getItem(storageKey) ?? "{}",
    ) as Choices;
    if (!value || typeof value !== "object") return {};
    return Object.fromEntries(
      Object.entries(value).filter(
        ([key, id]) =>
          ["network", "temperature", "gpu"].includes(key) &&
          typeof id === "string" &&
          id.length <= 128,
      ),
    );
  } catch {
    return {};
  }
}
function DeviceSelector({
  label,
  devices,
  value,
  onChange,
  empty,
}: {
  label: string;
  devices: Device[];
  value?: string;
  onChange: (id: string) => void;
  empty?: string;
}) {
  return (
    <label class="metric-selector">
      <span>{label}</span>
      <select
        aria-label={label}
        disabled={devices.length === 0}
        title={devices.find((device) => device.id === value)?.name}
        value={value ?? ""}
        onChange={(event) => onChange(event.currentTarget.value)}
      >
        {!value && <option value="">{empty ?? "No device available"}</option>}
        {devices.map((device) => (
          <option key={device.id} value={device.id}>
            {device.name}
          </option>
        ))}
      </select>
    </label>
  );
}
function DetailReading({
  state,
  device,
  metric,
  unit,
  binary = false,
  now,
  stale,
  label,
}: {
  state?: MonitoringState;
  device: Device;
  metric: MetricKind;
  unit: Unit;
  binary?: boolean;
  now?: number;
  stale: boolean;
  label?: string;
}) {
  const reading = currentReading(state, device.id, metric, now, stale);
  return (
    <div class="metric-detail-row">
      <span>{label ?? device.name}</span>
      <span>
        {formatMetric(reading?.value, unit, binary)}
        <ReadingStatus
          state={reading?.state ?? "unavailable"}
          lastSuccessAt={reading?.lastSuccessAt}
        />
      </span>
    </div>
  );
}
export function Dashboard({ onUnauthorized }: { onUnauthorized: () => void }) {
  const { state, loading, error, stale, serverNow, retry } =
    useMonitoring(onUnauthorized);
  const [choices, setChoices] = useState<Choices>(readChoices);
  const devices = state?.inventory.devices ?? [];
  const [customizing, setCustomizing] = useState(false);
  const disks = useDiskSelection(devices);
  const interfaces = devices.filter((device) => device.kind === "interface");
  const sensors = devices.filter((device) =>
    state?.inventory.series.some(
      (series) =>
        series.deviceId === device.id && series.metric === "temperature",
    ),
  );
  const gpus = devices.filter((device) => device.kind === "gpu");
  const cpu = devices.find((device) => device.kind === "host");
  const ram = devices.find((device) => device.kind === "memory");
  const network = selectDevice(interfaces, choices.network);
  const temperature = selectDevice(sensors, choices.temperature, true);
  const gpu =
    gpus.find((device) => device.id === choices.gpu) ??
    selectDevice(
      gpus.filter(
        (device) =>
          currentReading(state, device.id, "gpu_busy", serverNow, stale)
            ?.state === "available",
      ),
    ) ??
    selectDevice(gpus);
  const disk = devices.find(
    (device) => device.kind === "block" && device.default,
  );
  function choose(kind: keyof Choices, id: string) {
    const next = { ...choices, [kind]: id || undefined };
    setChoices(next);
    try {
      localStorage.setItem(storageKey, JSON.stringify(next));
    } catch {
      /* In-memory selections remain usable. */
    }
  }
  const removed = (Object.keys(choices) as (keyof Choices)[]).filter((kind) => {
    const list =
      kind === "network" ? interfaces : kind === "temperature" ? sensors : gpus;
    return (
      choices[kind] &&
      state?.inventory.revision &&
      !list.some((device) => device.id === choices[kind])
    );
  });
  const read = (device: Device | undefined, metric: MetricKind) =>
    currentReading(state, device?.id, metric, serverNow, stale);
  const value = (
    device: Device | undefined,
    metric: MetricKind,
    unit: Unit,
    binary = false,
  ) => formatMetric(read(device, metric)?.value, unit, binary);
  const status = (device: Device | undefined, metric: MetricKind): State =>
    read(device, metric)?.state ?? (loading ? "collecting" : "unavailable");
  const chart = (
    device: Device | undefined,
    metric: MetricKind,
    label: string,
    unit: Unit,
  ) => (
    <MetricChart
      key={device?.id ?? "missing"}
      series={[chartSeries(state, device?.id, metric, label)]}
      unit={unit}
    />
  );
  const detailProps = { state, now: serverNow, stale };
  const metricTime = uptime(state?.host.bootTime, serverNow);
  const disabled = state?.coverage.some((item) => item.reason === "disabled");
  const tileSources = new Set([
    "cpu",
    "memory",
    "network",
    "disks",
    "sensors",
    "gpu",
    "nvidia",
    ...devices.map((device) => device.id),
  ]);
  const generalSources = (state?.coverage ?? [])
    .filter((item) => !tileSources.has(item.source))
    .map((item) => item.source);
  return (
    <section class="host-dashboard">
      <div class="page-heading host-dashboard-heading">
        <div>
          <h1>Dashboard</h1>
          <p class="muted">
            {state?.host.name ?? "Host monitoring"}
            {state && " · " + state.host.os}
            {metricTime && " · " + metricTime}
          </p>
        </div>
        <div class="dashboard-toolbar">
          <div
            class="monitoring-freshness"
            data-stale={stale}
            data-active={!disabled && !loading && !!state}
          >
            <strong>
              {disabled
                ? "Monitoring disabled"
                : loading
                  ? "Connecting to host"
                  : stale
                    ? "Updates delayed"
                    : state
                      ? "Receiving metrics"
                      : "Metrics unavailable"}
            </strong>
            <span>2-second updates · 5-minute history</span>
          </div>
          <button
            type="button"
            class="dashboard-customize"
            onClick={() => setCustomizing(true)}
          >
            Customize dashboard
          </button>
        </div>
      </div>
      {error && (
        <div class="monitoring-error">
          {error}
          <button onClick={retry}>Retry now</button>
        </div>
      )}
      {removed.length > 0 && (
        <p class="selection-notice" role="status">
          Selected {removed.join(", ")} device is no longer available. Using the
          default when available.
        </p>
      )}
      <Dialog
        open={customizing}
        title="Customize dashboard"
        onClose={() => setCustomizing(false)}
      >
        <div class="dashboard-customization">
          <DeviceSelector
            label="Network interface"
            devices={interfaces}
            value={network?.id}
            onChange={(id) => choose("network", id)}
          />
          <p class="muted">Used for both download and upload.</p>
          <DeviceSelector
            label="Temperature sensor"
            devices={sensors}
            value={temperature?.id}
            onChange={(id) => choose("temperature", id)}
            empty="CPU sensor unavailable"
          />
          <DeviceSelector
            label="GPU device"
            devices={gpus}
            value={gpu?.id}
            onChange={(id) => choose("gpu", id)}
          />
          <DiskPicker selection={disks} />
          <MetricAvailability
            coverage={state?.coverage}
            sources={generalSources}
          />
        </div>
        <div class="dialog-actions">
          <button type="button" onClick={() => setCustomizing(false)}>
            Done
          </button>
        </div>
      </Dialog>
      {!loading && state && disks.missing.length > 0 && (
        <p class="selection-notice" role="status">
          A selected disk is unavailable. Open Customize dashboard to update
          your selection.
        </p>
      )}
      <div class="host-metric-grid">
        {(["Download", "Upload"] as const).map((label) => {
          const metric =
            label === "Download" ? "network_receive_rate" : "network_send_rate";
          return (
            <MetricCard
              key={label}
              label={label}
              availability={
                <MetricAvailability
                  coverage={state?.coverage}
                  sources={["network", network?.id]}
                  reading={read(network, metric)}
                />
              }
              value={value(network, metric, "bytes_per_second")}
              source={network?.name ?? "Host interface"}
              state={status(network, metric)}
              lastSuccessAt={read(network, metric)?.lastSuccessAt}
              chart={chart(network, metric, label, "bytes_per_second")}
              details={
                <>
                  {interfaces.map((device) => (
                    <DetailReading
                      key={device.id}
                      {...detailProps}
                      device={device}
                      metric={metric}
                      unit="bytes_per_second"
                    />
                  ))}
                  <p class="muted">
                    Includes LAN traffic; this is not an internet speed test.
                  </p>
                </>
              }
            />
          );
        })}
        <MetricCard
          label="Temperature"
          value={value(temperature, "temperature", "celsius")}
          source={temperature?.name ?? "CPU temperature"}
          state={status(temperature, "temperature")}
          lastSuccessAt={read(temperature, "temperature")?.lastSuccessAt}
          chart={
            temperature &&
            chart(temperature, "temperature", "Temperature", "celsius")
          }
          availability={
            <MetricAvailability
              coverage={state?.coverage}
              sources={
                temperature?.kind === "gpu"
                  ? [temperature.id]
                  : ["sensors", temperature?.id]
              }
              reading={read(temperature, "temperature")}
            />
          }
          details={
            <>
              {sensors.map((device) => (
                <DetailReading
                  key={device.id}
                  {...detailProps}
                  device={device}
                  metric="temperature"
                  unit="celsius"
                />
              ))}
            </>
          }
        />
        <MetricCard
          label="Disk I/O"
          availability={
            <MetricAvailability
              coverage={state?.coverage}
              sources={["disks", disk?.id]}
              reading={read(disk, "disk_read_rate")}
            />
          }
          value={
            value(disk, "disk_read_rate", "bytes_per_second") === "—"
              ? "—"
              : value(disk, "disk_read_rate", "bytes_per_second") + " read"
          }
          source={
            value(disk, "disk_write_rate", "bytes_per_second") +
            " write · " +
            (disk?.name ?? "Physical disks")
          }
          state={status(disk, "disk_read_rate")}
          lastSuccessAt={read(disk, "disk_read_rate")?.lastSuccessAt}
          detailLabel="Disk activity details"
          chart={
            <MetricChart
              series={[
                chartSeries(state, disk?.id, "disk_read_rate", "Read"),
                chartSeries(state, disk?.id, "disk_write_rate", "Write"),
              ]}
              unit="bytes_per_second"
            />
          }
          details={
            <>
              {devices
                .filter((device) => device.kind === "block" && !device.default)
                .map((device) => (
                  <div key={device.id}>
                    <DetailReading
                      {...detailProps}
                      device={device}
                      metric="disk_read_rate"
                      unit="bytes_per_second"
                      label={device.name + " · read"}
                    />
                    <DetailReading
                      {...detailProps}
                      device={device}
                      metric="disk_write_rate"
                      unit="bytes_per_second"
                      label={device.name + " · write"}
                    />
                  </div>
                ))}
              <p class="muted">
                Physical disks counted once. Partitions and stacked devices are
                excluded from the aggregate.
              </p>
            </>
          }
        />
        {disks.visible.map((device) => (
          <DiskFullnessCard
            key={device.id}
            device={device}
            state={state}
            stale={stale}
            now={serverNow}
          />
        ))}
        {!disks.visible.length && (
          <div class="disk-empty">
            <strong>Disk fullness</strong>
            <p>
              {loading
                ? "Discovering disks…"
                : disks.missing.length
                  ? "Your selected disks are unavailable."
                  : disks.filesystems.length
                    ? "No disks selected."
                    : "No disks available."}
            </p>
            <button type="button" onClick={() => setCustomizing(true)}>
              Choose disks
            </button>
          </div>
        )}
        <MetricCard
          label="RAM usage"
          availability={
            <MetricAvailability
              coverage={state?.coverage}
              sources={["memory", ram?.id]}
              reading={read(ram, "memory_percent")}
            />
          }
          gauge={{ value: read(ram, "memory_percent")?.value }}
          value={value(ram, "memory_percent", "percent")}
          source={
            value(ram, "memory_used", "bytes", true) +
            " / " +
            value(ram, "memory_total", "bytes", true)
          }
          state={status(ram, "memory_percent")}
          lastSuccessAt={read(ram, "memory_percent")?.lastSuccessAt}
          detailLabel="Memory details"
          chart={ram && chart(ram, "memory_percent", "RAM", "percent")}
          details={
            ram ? (
              <div>
                <DetailReading
                  {...detailProps}
                  device={ram}
                  metric="memory_available"
                  unit="bytes"
                  binary
                  label="Available"
                />
                <DetailReading
                  {...detailProps}
                  device={ram}
                  metric="swap_used"
                  unit="bytes"
                  binary
                  label="Swap used"
                />
                <DetailReading
                  {...detailProps}
                  device={ram}
                  metric="swap_total"
                  unit="bytes"
                  binary
                  label="Swap total"
                />
              </div>
            ) : (
              <p>No memory source available.</p>
            )
          }
        />
        <MetricCard
          label="CPU usage"
          availability={
            <MetricAvailability
              coverage={state?.coverage}
              sources={["cpu", cpu?.id]}
              reading={read(cpu, "cpu_busy")}
            />
          }
          gauge={{ value: read(cpu, "cpu_busy")?.value }}
          value={value(cpu, "cpu_busy", "percent")}
          source={cpu?.name ?? "All CPUs"}
          state={status(cpu, "cpu_busy")}
          lastSuccessAt={read(cpu, "cpu_busy")?.lastSuccessAt}
          detailLabel="CPU details"
          chart={cpu && chart(cpu, "cpu_busy", "CPU", "percent")}
          details={
            <div class="metric-core-list">
              {devices
                .filter((device) => device.kind === "cpu")
                .map((device) => (
                  <DetailReading
                    key={device.id}
                    {...detailProps}
                    device={device}
                    metric="cpu_busy"
                    unit="percent"
                  />
                ))}
            </div>
          }
        />
        <MetricCard
          label="GPU usage"
          availability={
            <MetricAvailability
              coverage={state?.coverage}
              sources={["gpu", "nvidia", ...gpus.map((device) => device.id)]}
              reading={read(gpu, "gpu_busy")}
            />
          }
          gauge={{ value: read(gpu, "gpu_busy")?.value }}
          value={value(gpu, "gpu_busy", "percent")}
          source={gpu?.name ?? "No GPU reading available"}
          state={status(gpu, "gpu_busy")}
          lastSuccessAt={read(gpu, "gpu_busy")?.lastSuccessAt}
          detailLabel="GPU details"
          chart={gpu && chart(gpu, "gpu_busy", "GPU", "percent")}
          note={
            gpu?.utilizationBasis === "busiest_engine"
              ? "Busiest measured engine"
              : undefined
          }
          details={
            <>
              {gpus.map((device) => (
                <DetailReading
                  key={device.id}
                  {...detailProps}
                  device={device}
                  metric="gpu_busy"
                  unit="percent"
                />
              ))}
              {gpu && (
                <>
                  <DetailReading
                    {...detailProps}
                    device={gpu}
                    metric="gpu_memory_used"
                    unit="bytes"
                    binary
                    label="GPU memory used"
                  />
                  <DetailReading
                    {...detailProps}
                    device={gpu}
                    metric="gpu_memory_total"
                    unit="bytes"
                    binary
                    label="GPU memory capacity"
                  />
                  <DetailReading
                    {...detailProps}
                    device={gpu}
                    metric="temperature"
                    unit="celsius"
                    label="GPU temperature"
                  />
                  {devices
                    .filter((device) => device.parentId === gpu.id)
                    .map((device) => (
                      <DetailReading
                        key={device.id}
                        {...detailProps}
                        device={device}
                        metric="engine_busy"
                        unit="percent"
                      />
                    ))}
                </>
              )}
            </>
          }
        />
      </div>
      <div class="dashboard-time-note">
        <span>Open a tile for details</span>
        <span>Charts show the last 5 minutes</span>
      </div>
    </section>
  );
}
