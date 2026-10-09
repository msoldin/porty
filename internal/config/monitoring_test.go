package config

import "testing"

func TestMonitoringConfigUsesNativeDefaults(t *testing.T) {
	cfg, err := Load(nil, func(string) (string, bool) { return "", false })
	if err != nil || cfg.Monitoring.Mode != "native" || cfg.Monitoring.HostProc != "/host/proc" || cfg.Monitoring.HostSys != "/host/sys" || cfg.Monitoring.HostRoot != "/host/root" {
		t.Fatalf("monitoring defaults: %#v %v", cfg, err)
	}
}
func TestMonitoringConfigAppliesEnvironmentOverrides(t *testing.T) {
	env := map[string]string{"PORTY_MONITORING_MODE": "host", "PORTY_MONITORING_HOST_PROC": "/mnt/proc", "PORTY_MONITORING_HOST_SYS": "/mnt/sys", "PORTY_MONITORING_HOST_ROOT": "/mnt/root"}
	cfg, err := Load(nil, func(key string) (string, bool) { value, ok := env[key]; return value, ok })
	if err != nil || cfg.Monitoring.Mode != "host" || cfg.Monitoring.HostProc != "/mnt/proc" || cfg.Monitoring.HostRoot != "/mnt/root" {
		t.Fatalf("monitoring overrides: %#v %v", cfg, err)
	}
}
func TestMonitoringConfigRejectsInvalidModeAndRoots(t *testing.T) {
	for _, env := range []map[string]string{{"PORTY_MONITORING_MODE": "container"}, {"PORTY_MONITORING_HOST_PROC": "relative"}, {"PORTY_MONITORING_HOST_SYS": ""}} {
		if _, err := Load(nil, func(key string) (string, bool) { value, ok := env[key]; return value, ok }); err == nil {
			t.Fatalf("accepted %#v", env)
		}
	}
}
