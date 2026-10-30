package ec2

import (
	"strings"
	"testing"
)

func validBootstrapConfig() BootstrapConfig {
	return BootstrapConfig{
		ControllerURL: "https://controller.example.test",
		WorkerAgentURL: "https://artifacts.example.test/worker-agent",
		WorkerAgentSHA256: strings.Repeat("a", 64),
		CloudHypervisorURL: "https://artifacts.example.test/cloud-hypervisor",
		CloudHypervisorSHA256: strings.Repeat("b", 64),
		DiskURL: "https://artifacts.example.test/worker.img",
		DiskSHA256: strings.Repeat("d", 64),
		SecretParameter: "/aiori/worker/shared-secret",
		ControllerSecurityGroupID: "sg-controller123",
		ImageID: "ubuntu-worker",
		MemoryLimitMiB: 4096,
		AgentPort: 9090,
	}
}

func TestBuildUserDataInstallsVerifiedAgentAndUsesSSMSecret(t *testing.T) {
	script, err := validBootstrapConfig().BuildUserData()
	if err != nil { t.Fatal(err) }
	for _, expected := range []string{
		"sha256sum --check --status", "aws ssm get-parameter", "--with-decryption",
		"x86_64", "apt-get", "worker-agent", "cloud-hypervisor", "ovmf", "firmware_path", "/api/v1/worker/heartbeat",
		"worker_endpoint", "worker_name", "aiori-worker-agent.service", "systemctl enable --now",
		"https://controller.example.test/api/v1/worker/metrics", "rstrip(\'%\')",
	} {
		if !strings.Contains(script, expected) { t.Fatalf("bootstrap script omitted %q", expected) }
	}
	if strings.Contains(script, "%!") { t.Fatal("bootstrap format template contains unresolved formatting directives") }
	if strings.Contains(script, "VM_CONTROLLER_WORKER_SHARED_SECRET=supersecret") { t.Fatal("bootstrap must not embed a long-lived worker secret") }
}

func TestBuildUserDataRejectsUnsafeArtifactAndMissingNetworkIdentity(t *testing.T) {
	cases := []struct{name string; mutate func(*BootstrapConfig)}{
		{"non-HTTPS artifact", func(c *BootstrapConfig) { c.WorkerAgentURL = "http://artifacts.example.test/agent" }},
		{"query-bearing artifact URL", func(c *BootstrapConfig) { c.DiskURL = "https://artifacts.example.test/disk?token=secret" }},
		{"invalid checksum", func(c *BootstrapConfig) { c.DiskSHA256 = "not-a-checksum" }},
		{"unsafe image ID", func(c *BootstrapConfig) { c.ImageID = "../root" }},
		{"invalid SSM parameter", func(c *BootstrapConfig) { c.SecretParameter = "secret-name" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validBootstrapConfig()
			tc.mutate(&cfg)
			if _, err := cfg.BuildUserData(); err == nil { t.Fatal("expected bootstrap validation error") }
		})
	}
}


func TestBuildUserDataAllowsTelemetryOnlyBootstrapWithoutControllerSecurityGroup(t *testing.T) {
	cfg := validBootstrapConfig()
	cfg.ControllerSecurityGroupID = ""
	script, err := cfg.BuildUserData()
	if err != nil { t.Fatalf("telemetry-only bootstrap should not require controller VPC security group: %v", err) }
	if !strings.Contains(script, "/api/v1/worker/metrics") { t.Fatal("bootstrap omitted platform telemetry collector") }
}
