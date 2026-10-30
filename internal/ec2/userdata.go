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
	WorkerSharedSecret string
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
		DiskSHA256: strings.TrimSpace(getenv("VM_CONTROLLER_WORKER_DISK_SHA256")),		ControllerSecurityGroupID: strings.TrimSpace(getenv("VM_CONTROLLER_SECURITY_GROUP_ID")),
		ImageID: strings.TrimSpace(getenv("VM_CONTROLLER_WORKER_IMAGE_ID")),
		MemoryLimitMiB: envUint64("VM_CONTROLLER_WORKER_MEMORY_LIMIT_MIB", 4096),
		AgentPort: int(envUint64("VM_CONTROLLER_WORKER_AGENT_PORT", 9090)),
	}
}

// Enabled reports whether enough configuration exists to generate a complete
// worker bootstrap. A partial configuration is intentionally not used.
func (b BootstrapConfig) Enabled() bool {
	return b.ControllerURL != "" && b.WorkerAgentURL != "" && b.WorkerAgentSHA256 != "" &&
		b.CloudHypervisorURL != "" && b.CloudHypervisorSHA256 != "" &&
		b.DiskURL != "" && b.DiskSHA256 != "" && b.WorkerSharedSecret != "" && b.ImageID != ""
}

func (b BootstrapConfig) BuildUserData() (string, error) {
	if !b.Enabled() { return "", errors.New("worker bootstrap is not configured; set verified worker artifacts, controller shared secret, and worker image ID") }
	if strings.TrimSpace(b.ControllerURL) == "" { return "", errors.New("controller URL is required; enter the public ngrok HTTPS URL on the AWS launch form") }
	if len(b.WorkerSharedSecret) < 32 || strings.ContainsAny(b.WorkerSharedSecret, "\r\n") { return "", errors.New("controller worker shared secret must contain at least 32 characters and no line breaks") }
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
	if b.ControllerSecurityGroupID != "" && !validID(b.ControllerSecurityGroupID, "sg-") { return "", errors.New("VM_CONTROLLER_SECURITY_GROUP_ID must be a valid security group ID in the worker VPC") }
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
	memoryBytes := b.MemoryLimitMiB * 1024 * 1024
	return fmt.Sprintf(`#!/bin/bash
set -Eeuo pipefail
exec > >(tee -a /var/log/aiori-worker-bootstrap.log | logger -t aiori-worker-bootstrap -s 2>/dev/console) 2>&1
export DEBIAN_FRONTEND=noninteractive
if [ "$(uname -m)" != "x86_64" ]; then echo "AIORI worker artifacts require an x86_64 AMI"; exit 1; fi
if [ ! -x /usr/bin/apt-get ]; then echo "AIORI worker bootstrap requires an Ubuntu/Debian AMI"; exit 1; fi
apt-get update
apt-get install -y ca-certificates curl ovmf
apt-get install -y python3
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
WORKER_SECRET=%s
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
cat >/usr/local/bin/aiori-metrics-collector.py <<'PYMETRICS'
#!/usr/bin/env python3
import datetime, json, os, subprocess, time, urllib.request
def cpu():
    v=list(map(int,open('/proc/stat').readline().split()[1:])); return sum(v),v[3]+(v[4] if len(v)>4 else 0)
def net():
    rx=tx=0
    for line in open('/proc/net/dev').readlines()[2:]:
        name,data=line.split(':',1)
        if name.strip()=='lo': continue
        v=list(map(int,data.split())); rx+=v[0]; tx+=v[8]
    return rx,tx
previous=cpu()
while True:
    time.sleep(60)
    current=cpu(); total=current[0]-previous[0]; idle=current[1]-previous[1]; previous=current
    cpu_pct=max(0,min(100,(total-idle)*100/total)) if total else 0
    mem={}
    for line in open('/proc/meminfo'):
        k,v=line.split(':',1); mem[k]=int(v.strip().split()[0])
    memory_pct=max(0,min(100,(mem.get('MemTotal',1)-mem.get('MemAvailable',mem.get('MemFree',0)))*100/mem.get('MemTotal',1)))
    disk_pct=float(subprocess.check_output(['df','-P','/']).decode().splitlines()[1].split()[4].rstrip('%%'))
    rx,tx=net()
    secret=open('/etc/aiori/worker.env').read().split('=',1)[1].strip()
    body={'instance_id':open('/var/lib/aiori-worker/instance-id').read().strip(),'timestamp':datetime.datetime.now(datetime.timezone.utc).isoformat(),'cpu_percent':cpu_pct,'memory_percent':memory_pct,'disk_percent':disk_pct,'network_rx_bytes':rx,'network_tx_bytes':tx}
    req=urllib.request.Request(os.environ['AIORI_METRICS_URL'],data=json.dumps(body).encode(),headers={'Authorization':'Bearer '+secret,'Content-Type':'application/json','ngrok-skip-browser-warning':'true'},method='POST')
    try: urllib.request.urlopen(req,timeout=10).read()
    except Exception as exc: print('metrics upload failed:',exc,flush=True)
PYMETRICS
printf '%%s' "$INSTANCE_ID" > /var/lib/aiori-worker/instance-id
cat >/etc/systemd/system/aiori-metrics-collector.service <<'EOF'
[Unit]
Description=AIORI in-platform host metrics collector
After=network-online.target aiori-worker-agent.service
Wants=network-online.target
[Service]
Type=simple
User=root
Environment=AIORI_METRICS_URL=%s/api/v1/worker/metrics
ExecStart=/usr/bin/python3 /usr/local/bin/aiori-metrics-collector.py
Restart=always
RestartSec=10
[Install]
WantedBy=multi-user.target
EOF
chmod 0755 /usr/local/bin/aiori-metrics-collector.py
systemctl daemon-reload
systemctl enable --now aiori-metrics-collector

`,
		shellQuote(b.WorkerAgentURL), shellQuote(strings.ToLower(b.WorkerAgentSHA256)),
		shellQuote(b.CloudHypervisorURL), shellQuote(strings.ToLower(b.CloudHypervisorSHA256)),
		shellQuote(b.DiskURL), shellQuote(strings.ToLower(b.DiskSHA256)),
		shellQuote(b.WorkerSharedSecret), b.NodeName, b.AgentPort, memoryBytes, b.ControllerURL, b.AgentPort, b.ImageID, b.ControllerURL), nil
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
