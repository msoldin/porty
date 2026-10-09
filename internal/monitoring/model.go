package monitoring

import (
	"context"
	"time"
)

const (
	SampleInterval   = 2 * time.Second
	HistoryWindow    = 5 * time.Minute
	StaleAfter       = 6 * time.Second
	maxSamples       = 151
	maxStoredBytes   = 64 << 20
	maxResponseBytes = 8 << 20
	maxSeries        = 4096
	maxSources       = 512
)

type State string

const (
	StateCollecting  State = "collecting"
	StateAvailable   State = "available"
	StateUnavailable State = "unavailable"
	StateStale       State = "stale"
)

type Unit string

const (
	UnitPercent Unit = "percent"
	UnitBytes   Unit = "bytes"
	UnitRate    Unit = "bytes_per_second"
	UnitCelsius Unit = "celsius"
)

type DeviceKind string

const (
	DeviceHost       DeviceKind = "host"
	DeviceCPU        DeviceKind = "cpu"
	DeviceMemory     DeviceKind = "memory"
	DeviceInterface  DeviceKind = "interface"
	DeviceBlock      DeviceKind = "block"
	DeviceFilesystem DeviceKind = "filesystem"
	DeviceSensor     DeviceKind = "sensor"
	DeviceGPU        DeviceKind = "gpu"
	DeviceEngine     DeviceKind = "gpu_engine"
)

type MetricKind string

const (
	MetricCPUBusy             MetricKind = "cpu_busy"
	MetricMemoryTotal         MetricKind = "memory_total"
	MetricMemoryAvailable     MetricKind = "memory_available"
	MetricMemoryUsed          MetricKind = "memory_used"
	MetricMemoryPercent       MetricKind = "memory_percent"
	MetricSwapTotal           MetricKind = "swap_total"
	MetricSwapUsed            MetricKind = "swap_used"
	MetricTemperature         MetricKind = "temperature"
	MetricGPUBusy             MetricKind = "gpu_busy"
	MetricGPUMemoryTotal      MetricKind = "gpu_memory_total"
	MetricGPUMemoryUsed       MetricKind = "gpu_memory_used"
	MetricEngineBusy          MetricKind = "engine_busy"
	MetricNetworkReceive      MetricKind = "network_receive_rate"
	MetricNetworkSend         MetricKind = "network_send_rate"
	MetricDiskRead            MetricKind = "disk_read_rate"
	MetricDiskWrite           MetricKind = "disk_write_rate"
	MetricFilesystemTotal     MetricKind = "filesystem_total"
	MetricFilesystemUsed      MetricKind = "filesystem_used"
	MetricFilesystemAvailable MetricKind = "filesystem_available"
	MetricFilesystemPercent   MetricKind = "filesystem_percent"
)

type Reading struct {
	Value         *float64   `json:"value"`
	State         State      `json:"state"`
	Reason        string     `json:"reason,omitempty"`
	SampledAt     time.Time  `json:"sampledAt"`
	LastSuccessAt *time.Time `json:"lastSuccessAt,omitempty"`
}
type Series struct {
	ID       string     `json:"id"`
	DeviceID string     `json:"deviceId"`
	Metric   MetricKind `json:"metric"`
	Unit     Unit       `json:"unit"`
}
type Device struct {
	ID               string     `json:"id"`
	ParentID         string     `json:"parentId,omitempty"`
	Kind             DeviceKind `json:"kind"`
	Name             string     `json:"name"`
	Driver           string     `json:"driver,omitempty"`
	MountPaths       []string   `json:"mountPaths,omitempty"`
	Default          bool       `json:"default"`
	UtilizationBasis string     `json:"utilizationBasis,omitempty"`
}
type Coverage struct {
	Source  string `json:"source"`
	Omitted int    `json:"omitted"`
	Partial bool   `json:"partial"`
	Reason  string `json:"reason,omitempty"`
}
type Inventory struct {
	Revision string   `json:"revision"`
	Devices  []Device `json:"devices"`
	Series   []Series `json:"series"`
}
type HostInfo struct {
	Name     string     `json:"name"`
	OS       string     `json:"os"`
	BootTime *time.Time `json:"bootTime,omitempty"`
}
type Sample struct {
	Sequence   string             `json:"sequence"`
	CapturedAt time.Time          `json:"capturedAt"`
	Readings   map[string]Reading `json:"readings"`
}
type Snapshot struct {
	Generation  string     `json:"generation"`
	Cursor      string     `json:"cursor"`
	Reset       bool       `json:"reset"`
	ServerTime  time.Time  `json:"serverTime"`
	WindowStart time.Time  `json:"windowStart"`
	Host        HostInfo   `json:"host"`
	Inventory   *Inventory `json:"inventory,omitempty"`
	Current     Sample     `json:"current"`
	Samples     []Sample   `json:"samples"`
	Coverage    []Coverage `json:"coverage"`
}
type Batch struct {
	Devices  []Device
	Series   []Series
	Readings map[string]Reading
	Coverage []Coverage
}
type Source interface {
	ID() string
	Collect(context.Context) (Batch, error)
	Close() error
}

func available(value float64, now time.Time) Reading {
	return Reading{Value: &value, State: StateAvailable, SampledAt: now, LastSuccessAt: &now}
}
func unavailable(reason string, now time.Time) Reading {
	return Reading{State: StateUnavailable, Reason: reason, SampledAt: now}
}
