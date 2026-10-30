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
		KernelURL: "https://artifacts.example.test/vmlinuz",
		KernelSHA256: strings.Repeat("c", 64),
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
		"worker-agent", "cloud-hypervisor", "/api/v1/worker/heartbeat",
		"worker_endpoint", "aiori-worker-agent.service", "systemctl enable --now"
	} {
		if !strings.Contains(script, expected) { t.Fatalf("bootstrap script omitted %q", expected) }
	}
	if strings.Contains(script, "VM_CONTROLLER_WORKER_SHARED_SECRET=supersecret") { t.Fatal("bootstrap must not embed a long-lived worker secret") }
}

func TestBuildUserDataRejectsUnsafeArtifactAndMissingNetworkIdentity(t *testing.T) {
	cases := []struct{name string; mutate func(*BootstrapConfig)}{
		{"non-HTTPS artifact", func(c *BootstrapConfig) { c.WorkerAgentURL = "http://artifacts.example.test/agent" }},
		{"query-bearing artifact URL", func(c *BootstrapConfig) { c.DiskURL = "https://artifacts.example.test/disk?token=secret" }},
		{"invalid checksum", func(c *BootstrapConfig) { c.KernelSHA256 = "not-a-checksum" }},
		{"missing controller security group", func(c *BootstrapConfig) { c.ControllerSecurityGroupID = "" }},
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
