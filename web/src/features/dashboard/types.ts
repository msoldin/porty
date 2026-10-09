export type State = "collecting" | "available" | "unavailable" | "stale";
export type Unit = "percent" | "bytes" | "bytes_per_second" | "celsius";
export type MetricKind =
  | "cpu_busy"
  | "memory_total"
  | "memory_available"
  | "memory_used"
  | "memory_percent"
  | "swap_total"
  | "swap_used"
  | "temperature"
  | "gpu_busy"
  | "gpu_memory_total"
  | "gpu_memory_used"
  | "engine_busy"
  | "network_receive_rate"
  | "network_send_rate"
  | "disk_read_rate"
  | "disk_write_rate"
  | "filesystem_total"
  | "filesystem_used"
  | "filesystem_available"
  | "filesystem_percent";
export type DeviceKind =
  | "host"
  | "cpu"
  | "memory"
  | "interface"
  | "block"
  | "filesystem"
  | "sensor"
  | "gpu"
  | "gpu_engine";
export type Reason =
  | "collecting"
  | "unsupported"
  | "build_unsupported"
  | "no_device"
  | "permission_denied"
  | "source_missing"
  | "read_failed"
  | "invalid_data"
  | "collection_delayed"
  | "counter_reset"
  | "limit_exceeded"
  | "disabled"
  | "partial_coverage"
  | "host_mount_unavailable";
export type Reading = {
  value: number | null;
  state: State;
  reason?: Reason;
  sampledAt: string;
  lastSuccessAt?: string;
};
export type Series = {
  id: string;
  deviceId: string;
  metric: MetricKind;
  unit: Unit;
};
export type Device = {
  id: string;
  parentId?: string;
  kind: DeviceKind;
  name: string;
  driver?: string;
  mountPaths?: string[];
  default: boolean;
  utilizationBasis?: "vendor" | "busiest_engine";
};
export type Coverage = {
  source: string;
  omitted: number;
  partial: boolean;
  reason?: Reason;
};
export type Inventory = {
  revision: string;
  devices: Device[];
  series: Series[];
};
export type HostInfo = { name: string; os: string; bootTime?: string };
export type Sample = {
  sequence: string;
  capturedAt: string;
  readings: Record<string, Reading>;
};
export type Snapshot = {
  generation: string;
  cursor: string;
  reset: boolean;
  serverTime: string;
  windowStart: string;
  host: HostInfo;
  inventory?: Inventory;
  current: Sample;
  samples: Sample[] | null;
  coverage: Coverage[] | null;
};
export type MonitoringState = Omit<
  Snapshot,
  "reset" | "inventory" | "samples" | "coverage"
> & { inventory: Inventory; samples: Sample[]; coverage: Coverage[] };
