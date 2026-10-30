package ec2

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// BootstrapConfig describes immutable, checksum-verified artifacts and the
// controller endpoint needed to turn a fresh Linux AMI into a worker agent.
type BootstrapConfig struct {
	ControllerURL string
	WorkerAgentURL string
	WorkerAgentSHA256 string
	CloudHypervisorURL string
	CloudHypervisorSHA256 string
	DiskURL string
	DiskSHA256 string
	SecretParameter string
	ControllerSecurityGroupID string
	ImageID string
	NodeName string
	MemoryLimitMiB uint64
	AgentPort int
	CloudWatchLogGroup string
}

var sha256Pattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)
var imageNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

func BootstrapConfigFromEnv() BootstrapConfig {
	return BootstrapConfig{
		ControllerURL: strings.TrimRight(strings.TrimSpace(getenv("VM_CONTROLLER_PUBLIC_URL")), "/"),
		WorkerAgentURL: strings.TrimSpace(getenv("VM_CONTROLLER_WORKER_AGENT_URL")),
		WorkerAgentSHA256: strings.TrimSpace(getenv("VM_CONTROLLER_WORKER_AGENT_SHA256")),
		CloudHypervisorURL: strings.TrimSpace(getenv("VM_CONTROLLER_CLOUD_HYPERVISOR_URL")),
		CloudHypervisorSHA256: strings.TrimSpace(getenv("VM_CONTROLLER_CLOUD_HYPERVISOR_SHA256")),
		DiskURL: strings.TrimSpace(getenv("VM_CONTROLLER_WORKER_DISK_URL")),
		DiskSHA256: strings.TrimSpace(getenv("VM_CONTROLLER_WORKER_DISK_SHA256")),
		SecretParameter: strings.TrimSpace(getenv("VM_CONTROLLER_WORKER_SECRET_PARAMETER")),
		ControllerSecurityGroupID: strings.TrimSpace(getenv("VM_CONTROLLER_SECURITY_GROUP_ID")),
		ImageID: strings.TrimSpace(getenv("VM_CONTROLLER_WORKER_IMAGE_ID")),
		MemoryLimitMiB: envUint64("VM_CONTROLLER_WORKER_MEMORY_LIMIT_MIB", 4096),
		AgentPort: int(envUint64("VM_CONTROLLER_WORKER_AGENT_PORT", 9090)),
		CloudWatchLogGroup: strings.TrimSpace(getenv("VM_CONTROLLER_WORKER_LOG_GROUP")),
	}
}

// Enabled reports whether enough configuration exists to generate a complete
// worker bootstrap. A partial configuration is intentionally not used.
func (b BootstrapConfig) Enabled() bool {
	return b.ControllerURL != "" && b.WorkerAgentURL != "" && b.WorkerAgentSHA256 != "" &&
		b.CloudHypervisorURL != "" && b.CloudHypervisorSHA256 != "" &&
		b.DiskURL != "" && b.DiskSHA256 != "" && b.SecretParameter != "" && b.ControllerSecurityGroupID != "" && b.ImageID != ""
}

func (b BootstrapConfig) BuildUserData() (string, error) {
	if !b.Enabled() { return "", errors.New("worker bootstrap is not configured; set controller URL, artifact URLs and SHA-256 checksums, worker secret parameter, and image ID") }
	for name, raw := range map[string]string{
		"controller URL": b.ControllerURL,
		"worker agent URL": b.WorkerAgentURL,
		"Cloud Hypervisor URL": b.CloudHypervisorURL,
		"disk URL": b.DiskURL,
	} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (name == "controller URL" && u.Path != "" && u.Path != "/") {
			return "", fmt.Errorf("%s must be an HTTPS URL without credentials, query, or fragment", name)
		}
	}
	for name, sum := range map[string]string{
		"worker agent": b.WorkerAgentSHA256, "Cloud Hypervisor": b.CloudHypervisorSHA256,
		"disk": b.DiskSHA256,
	} {
		if !sha256Pattern.MatchString(sum) { return "", fmt.Errorf("%s SHA-256 must be 64 hexadecimal characters", name) }
	}
	if !validID(b.ControllerSecurityGroupID, "sg-") { return "", errors.New("VM_CONTROLLER_SECURITY_GROUP_ID must be a valid security group ID in the worker VPC") }
	if b.NodeName == "" { b.NodeName = "aiori-worker" }
	if len(b.NodeName) > 128 { return "", errors.New("worker node name must be at most 128 characters") }
	for _, r := range b.NodeName {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == ' ') {
			return "", errors.New("worker node name contains unsupported characters")
		}
	}
	if !imageNamePattern.MatchString(b.ImageID) { return "", errors.New("worker image ID must be a safe identifier") }
	if !strings.HasPrefix(b.SecretParameter, "/") || len(b.SecretParameter) > 1011 || strings.ContainsAny(b.SecretParameter, "\r\n") {
		return "", errors.New("worker secret parameter must be an absolute SSM parameter path")
	}
	if b.MemoryLimitMiB < 512 || b.MemoryLimitMiB > 1<<20 { return "", errors.New("worker memory limit must be between 512 and 1048576 MiB") }
	if b.AgentPort < 1024 || b.AgentPort > 65535 { return "", errors.New("worker agent port must be between 1024 and 65535") }
	if b.CloudWatchLogGroup == "" { b.CloudWatchLogGroup = "/aiori/worker" }
	if !strings.HasPrefix(b.CloudWatchLogGroup, "/aiori/") || strings.ContainsAny(b.CloudWatchLogGroup, "\r\n\\\"") { return "", errors.New("worker CloudWatch log group must be a safe /aiori/ path") }
	memoryBytes := b.MemoryLimitMiB * 1024 * 1024
	return fmt.Sprintf(`#!/bin/bash
set -Eeuo pipefail
exec > >(tee -a /var/log/aiori-worker-bootstrap.log | logger -t aiori-worker-bootstrap -s 2>/dev/console) 2>&1
export DEBIAN_FRONTEND=noninteractive
if [ "$(uname -m)" != "x86_64" ]; then echo "AIORI worker artifacts require an x86_64 AMI"; exit 1; fi
if [ ! -x /usr/bin/apt-get ]; then echo "AIORI worker bootstrap requires an Ubuntu/Debian AMI"; exit 1; fi
apt-get update
apt-get install -y awscli ca-certificates curl ovmf
install -d -m 0755 /opt/aiori /etc/aiori /var/lib/aiori-worker/runtime /var/lib/aiori-worker/images
download_verified() {
  local url="$1" destination="$2" checksum="$3" mode="$4"
  curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 "$url" -o "$destination"
  echo "$checksum  $destination" | sha256sum --check --status
  chmod "$mode" "$destination"
}
download_verified %s /usr/local/bin/worker-agent %s 0755
download_verified %s /usr/local/bin/cloud-hypervisor %s 0755
download_verified %s /var/lib/aiori-worker/images/worker.img %s 0644
TOKEN=$(curl --fail --silent --show-error -X PUT -H 'X-aws-ec2-metadata-token-ttl-seconds: 21600' http://169.254.169.254/latest/api/token)
IMDS='http://169.254.169.254/latest/meta-data'
INSTANCE_ID=$(curl --fail --silent --show-error -H "X-aws-ec2-metadata-token: $TOKEN" "$IMDS/instance-id")
LOCAL_IPV4=$(curl --fail --silent --show-error -H "X-aws-ec2-metadata-token: $TOKEN" "$IMDS/local-ipv4")
REGION=$(curl --fail --silent --show-error -H "X-aws-ec2-metadata-token: $TOKEN" "$IMDS/placement/region")
CPU_COUNT=$(nproc)
WORKER_SECRET=$(aws ssm get-parameter --region "$REGION" --name %s --with-decryption --query 'Parameter.Value' --output text)
test -n "$WORKER_SECRET"
umask 077
printf 'VM_CONTROLLER_WORKER_SHARED_SECRET=%%s\n' "$WORKER_SECRET" > /etc/aiori/worker.env
unset WORKER_SECRET
cat > /etc/aiori/worker.json <<EOF
{
  "worker_id": "$INSTANCE_ID",
  "worker_name": "%s",
  "listen_addr": "0.0.0.0:%d",
  "runtime_dir": "/var/lib/aiori-worker/runtime",
  "cloud_hypervisor_path": "/usr/local/bin/cloud-hypervisor",
  "max_vcpus": $CPU_COUNT,
  "max_memory_bytes": %d,
  "request_timeout_ms": 2000,
  "startup_timeout_ms": 10000,
  "controller_heartbeat_url": "%s/api/v1/worker/heartbeat",
  "worker_endpoint": "http://$LOCAL_IPV4:%d",
  "heartbeat_interval_seconds": 10,
  "images": [{
    "id": "%s",
    "firmware_path": "/usr/share/OVMF/OVMF_CODE.fd",
    "disk_path": "/var/lib/aiori-worker/images/worker.img"
  }]
}
EOF
chmod 0600 /etc/aiori/worker.env /etc/aiori/worker.json
cat > /etc/systemd/system/aiori-worker-agent.service <<'EOF'
[Unit]
Description=AIORI worker agent
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
User=root
EnvironmentFile=/etc/aiori/worker.env
ExecStart=/usr/local/bin/worker-agent -config /etc/aiori/worker.json
Restart=always
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/lib/aiori-worker /run
PrivateTmp=true
[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now aiori-worker-agent
curl --fail --silent --show-error --location https://amazoncloudwatch-agent.s3.amazonaws.com/ubuntu/amd64/latest/amazon-cloudwatch-agent.deb -o /tmp/amazoncloudwatch-agent.deb
dpkg -i /tmp/amazoncloudwatch-agent.deb
cat > /opt/aws/amazon-cloudwatch-agent/etc/amazon-cloudwatch-agent.json <<CWEOF
{
  "agent": {"metrics_collection_interval": 60, "run_as_user": "root"},
  "metrics": {"namespace": "CWAgent", "append_dimensions": {"InstanceId": "$INSTANCE_ID"}, "aggregation_dimensions": [["InstanceId"]], "metrics_collected": {
    "mem": {"measurement": ["mem_used_percent", "mem_available"], "metrics_collection_interval": 60},
    "disk": {"measurement": ["used_percent", "inodes_free"], "resources": ["/"], "metrics_collection_interval": 60},
    "diskio": {"measurement": ["reads", "writes", "read_bytes", "write_bytes", "io_time"], "resources": ["*"], "metrics_collection_interval": 60},
    "net": {"measurement": ["bytes_sent", "bytes_recv", "packets_sent", "packets_recv"], "resources": ["*"], "metrics_collection_interval": 60},
    "cpu": {"measurement": ["usage_idle", "usage_user", "usage_system", "usage_iowait"], "totalcpu": true, "metrics_collection_interval": 60}
  }},
  "logs": {"logs_collected": {"files": {"collect_list": [
    {"file_path": "/var/log/aiori-worker-bootstrap.log", "log_group_name": "%s", "log_stream_name": "$INSTANCE_ID/bootstrap", "timezone": "UTC"},
    {"file_path": "/var/log/aiori-worker-agent.log", "log_group_name": "%s", "log_stream_name": "$INSTANCE_ID/agent", "timezone": "UTC"},
    {"file_path": "/var/log/syslog", "log_group_name": "%s", "log_stream_name": "$INSTANCE_ID/syslog", "timezone": "UTC"}
  ]}}}
}
CWEOF
/opt/aws/amazon-cloudwatch-agent/bin/amazon-cloudwatch-agent-ctl -a fetch-config -m ec2 -c file:/opt/aws/amazon-cloudwatch-agent/etc/amazon-cloudwatch-agent.json -s
`,
		shellQuote(b.WorkerAgentURL), shellQuote(strings.ToLower(b.WorkerAgentSHA256)),
		shellQuote(b.CloudHypervisorURL), shellQuote(strings.ToLower(b.CloudHypervisorSHA256)),
		shellQuote(b.DiskURL), shellQuote(strings.ToLower(b.DiskSHA256)),
		shellQuote(b.SecretParameter), b.NodeName, b.AgentPort, memoryBytes, b.ControllerURL, b.AgentPort, b.ImageID, b.CloudWatchLogGroup, b.CloudWatchLogGroup, b.CloudWatchLogGroup), nil
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

// getenv/envUint64 are kept local to avoid introducing a config dependency.
func getenv(key string) string { return os.Getenv(key) }
func envUint64(key string, fallback uint64) uint64 {
	value := strings.TrimSpace(getenv(key))
	if value == "" { return fallback }
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil { return fallback }
	return n
}
