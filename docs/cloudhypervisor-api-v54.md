# Cloud Hypervisor v54.0 API contract used by the adapter

The adapter targets Cloud Hypervisor **v54.0** and its versioned OpenAPI specification:

- [Cloud Hypervisor v54.0 OpenAPI specification](https://github.com/cloud-hypervisor/cloud-hypervisor/blob/v54.0/vmm/src/api/openapi/cloud-hypervisor.yaml)
- [Cloud Hypervisor API documentation](https://github.com/cloud-hypervisor/cloud-hypervisor/blob/v54.0/docs/api.md)

## Adapter endpoint contract

| Operation | Method | Path under `/api/v1` | Expected success |
|---|---|---|---|
| Create VM configuration | PUT | `/vm.create` | HTTP 204 |
| Boot created VM | PUT | `/vm.boot` | HTTP 204 |
| Shut down running VM | PUT | `/vm.shutdown` | HTTP 204 |
| Read VM info/state | GET | `/vm.info` | HTTP 200 JSON |
| Check VMM API | GET | `/vmm.ping` | HTTP 200 JSON |

The API is served over the local Unix socket configured with `--api-socket path=...`. It must not be exposed on a public network interface.

## Schema details that matter

- `VmConfig.payload` is required. The adapter requires either a non-empty kernel or firmware path.
- `VmConfig.memory.size`, when supplied, is an integer number of bytes. The Go model therefore uses `int64`, not a human-readable string.
- The VM state enum includes `Created`, `Running`, `Shutdown`, `Paused`, and `BreakPoint`; the adapter recognizes these values.
- Disk image identity and fixed path resolution are worker responsibilities. The adapter accepts a disk path from its caller and must not be treated as the image allowlist/security boundary.
- The adapter's fake-API tests verify endpoint methods/paths, Unix-socket transport, the JSON memory/disk shape, request timeout handling, and structured errors.

## Verification boundary

The versioned OpenAPI contract is the source for API shape and endpoint choices. Fake-API tests do not prove that a v54.0 binary is installed on an AWS worker or that a guest boots. Those host-specific checks remain a deployment acceptance gate before the two-worker rollout.
