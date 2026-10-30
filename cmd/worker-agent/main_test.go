package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeWorkerConfig(t *testing.T, endpoint string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "worker.json")
	body := `{
		"worker_id":"i-abc123",
		"worker_name":"aiori-node",
		"listen_addr":"0.0.0.0:9090",
		"runtime_dir":"/var/lib/aiori-worker/runtime",
		"cloud_hypervisor_path":"/usr/local/bin/cloud-hypervisor",
		"max_vcpus":4,
		"max_memory_bytes":4294967296,
		"controller_heartbeat_url":"https://controller.example.test/api/v1/worker/heartbeat",
		"worker_endpoint":"` + endpoint + `",
		"images":[{"id":"ubuntu-demo","kernel_path":"/var/lib/aiori-worker/images/vmlinuz","disk_path":"/var/lib/aiori-worker/images/worker.img"}]
	}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil { t.Fatal(err) }
	return path
}

func TestLoadConfigRequiresAdvertisedEndpointForHeartbeat(t *testing.T) {
	_, err := loadConfig(writeWorkerConfig(t, ""))
	if err == nil || !strings.Contains(err.Error(), "worker_endpoint is required") { t.Fatalf("unexpected error: %v", err) }
}

func TestLoadConfigAcceptsPrivateWorkerEndpointAndName(t *testing.T) {
	cfg, err := loadConfig(writeWorkerConfig(t, "http://10.0.1.12:9090"))
	if err != nil { t.Fatal(err) }
	if cfg.WorkerID != "i-abc123" || cfg.WorkerName != "aiori-node" || cfg.WorkerEndpoint != "http://10.0.1.12:9090" { t.Fatalf("worker identity not loaded: %#v", cfg) }
}
