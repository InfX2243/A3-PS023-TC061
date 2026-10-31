# Demo, Evidence, and Teardown Runbook (T-21)

Use this only after both worker hosts pass the KVM/Cloud Hypervisor guest boot-stop gate and the services are deployed. The commands below are a procedure, not evidence that a deployment has already occurred.

## Before the demo

- [ ] Record commit SHA, Go version, controller/worker binary SHA-256, Cloud Hypervisor version/hash, guest image SHA-256, AMI IDs, kernels, region/AZ, and worker IDs.
- [ ] Confirm both worker heartbeats are healthy and recent.
- [ ] Confirm the operator UI is reached through the intended TLS boundary and worker ports are private.
- [ ] Confirm the approved image alias resolves to the documented immutable artifact.
- [ ] Create an evidence directory outside the repository for raw logs; sanitize before committing any evidence.
- [ ] Use a dedicated prototype account/environment and verify the teardown owner.

## API demo

Set the API base URL to the intended protected controller endpoint. Read the operator token into the environment without putting the literal token in this file or in shell history.

```sh
export API_BASE="https://controller.example.invalid"
read -rsp "Operator token: " VM_CONTROLLER_OPERATOR_TOKEN
echo
export VM_CONTROLLER_OPERATOR_TOKEN
curl --fail --silent --show-error "$API_BASE/healthz"
curl --fail --silent --show-error \
  -H "Authorization: Bearer $VM_CONTROLLER_OPERATOR_TOKEN" \
  "$API_BASE/api/v1/workers" | jq .
```

Create a VM using an approved image and a unique idempotency key:

```sh
export IDEMPOTENCY_KEY="demo-$(date +%s)"
CREATE_BODY='{"name":"demo-vm-01","vcpus":1,"memory_mib":1024,"image_id":"ubuntu-demo"}'
curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $VM_CONTROLLER_OPERATOR_TOKEN" \
  -H "Idempotency-Key: $IDEMPOTENCY_KEY" \
  -H "Content-Type: application/json" \
  -d "$CREATE_BODY" "$API_BASE/api/v1/vms" | tee create-response.json | jq .
export VM_ID="$(jq -r '.vm.id' create-response.json)"
export OP_ID="$(jq -r '.operation.id' create-response.json)"
```

Inspect the VM and operation, then start and stop it:

```sh
curl --fail --silent --show-error -H "Authorization: Bearer $VM_CONTROLLER_OPERATOR_TOKEN" "$API_BASE/api/v1/vms/$VM_ID" | jq .
curl --fail --silent --show-error -H "Authorization: Bearer $VM_CONTROLLER_OPERATOR_TOKEN" "$API_BASE/api/v1/operations/$OP_ID" | jq .
curl --fail-with-body --silent --show-error -X POST -H "Authorization: Bearer $VM_CONTROLLER_OPERATOR_TOKEN" "$API_BASE/api/v1/vms/$VM_ID/start" | jq .
curl --fail-with-body --silent --show-error -X POST -H "Authorization: Bearer $VM_CONTROLLER_OPERATOR_TOKEN" "$API_BASE/api/v1/vms/$VM_ID/stop" | jq .
curl --fail --silent --show-error -H "Authorization: Bearer $VM_CONTROLLER_OPERATOR_TOKEN" "$API_BASE/api/v1/vms/$VM_ID" | jq .
```

Repeat the same create request with the same idempotency key and body. The returned VM ID must match the original and the worker must not create a second VM. Reusing the same key with a different body should return a conflict.

## UI demo

1. Open the protected controller URL in a browser.
2. Sign in with the operator token.
3. Show both worker rows, recent heartbeat, available memory, and configured cap.
4. Prepare a VM using the approved image and supported resource values.
5. Show its selected worker and operation state.
6. Start and stop the VM; show the observed state changing only after worker confirmation.
7. Show a validation error for an unsupported image or out-of-range resource request.
8. Refresh while an operation is pending/unknown and show that uncertain outcomes are not reported as success.

## Fault and recovery evidence

Run these only in a disposable test environment:

- [ ] Concurrent create requests at the memory-capacity boundary: prove reservations do not exceed safe capacity.
- [ ] Repeat the same idempotency key and payload; then repeat the key with a different payload.
- [ ] Make a worker unavailable before dispatch and verify a safe error.
- [ ] Interrupt the worker response after dispatch and verify operation state becomes unknown and reservation is retained.
- [ ] Restart the controller with an unfinished operation and verify reconciliation queries worker state before resolving it.
- [ ] Restart a worker agent and verify stale/unreachable status is shown until a fresh heartbeat arrives.
- [ ] Attempt unauthenticated UI/API/worker requests and an unapproved image ID.
- [ ] Confirm child VMM processes are killed/reaped on orderly worker-agent shutdown.
- [ ] Capture sanitized service logs and state before/after each fault.

## Evidence package

Store the following, with timestamps and secrets removed:
- controller and worker binary SHA-256 values;
- VMM and guest artifact versions/checksums;
- sanitized worker heartbeat inventory;
- VM and operation IDs with create/start/stop state transitions;
- API/UI screenshots or saved JSON responses;
- test command output and fault-injection observations;
- known limitations and any skipped checks;
- actual AWS resource identifiers in a private operational record, not public source control.

Do not mark a scenario passed if it was not executed. Distinguish simulated-worker tests from real KVM-backed guest tests.

## Teardown checklist

1. Stop demo VMs through the controller and verify the final reported state.
2. Stop worker-agent services, then controller service.
3. Preserve sanitized evidence and checksums.
4. From an authenticated AWS CLI session, enumerate only resources with the prototype's agreed tags.
5. Verify instance/volume IDs against the deployment record before terminating/deleting them.
6. Delete only disposable prototype volumes/snapshots after evidence retention is confirmed.
7. Confirm no billable prototype resources remain and record the teardown timestamp/result.
8. Unset operator/shared-secret environment variables and remove temporary response files that contain operational details.

Never run an account-wide deletion command or guess AWS resource IDs.
