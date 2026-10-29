# Security and Operational Checklist (T-18)

This checklist separates controls implemented in code from deployment checks that must be confirmed when the prototype is installed. A code-level control does not prove the deployed host/network is configured correctly.

## Implemented in the repository

- [x] Operator API requires a bearer token and compares it in constant time.
- [x] Worker heartbeat and lifecycle APIs require the shared bearer secret; an empty secret fails closed.
- [x] Operator tokens and worker shared secrets are read from environment variables, not JSON configuration.
- [x] Controller API and worker lifecycle JSON bodies are bounded and reject unknown fields.
- [x] VM image IDs are checked against controller and worker allowlists. The browser never supplies a host image path or arbitrary Cloud Hypervisor arguments.
- [x] VM IDs are constrained before being used in runtime paths. Cloud Hypervisor is started with an argument array, not a shell command.
- [x] Worker base URLs are explicit configuration, reject URL credentials/query/fragment, and redirects are disabled for worker calls.
- [x] Worker runtime directories are forced to mode 0700; the Cloud Hypervisor API socket is set to 0600.
- [x] UI sessions use an HMAC-signed expiry cookie with HttpOnly, SameSite=Strict, and configurable Secure; pages set CSP, anti-framing, no-sniff, and referrer-policy headers.
- [x] UI form payloads are bounded and user-facing errors avoid exposing internal database/worker errors.
- [x] Worker outcome timeouts are represented as unknown and do not release memory reservations.
- [x] Structured logs do not include bearer tokens or request bodies.

## Required deployment checks (pending)

- [ ] Put controller and worker endpoints on private networking; allow worker API ingress only from the controller security group/address.
- [ ] Expose the operator UI/API only through a TLS endpoint with a restricted operator access path.
- [ ] Set secure_cookies: true when the UI is served through HTTPS. Do not expose a plain-HTTP UI with operator login enabled on an untrusted network.
- [ ] Install config files owned by the service account/root and not group/world writable; inject secrets through a protected environment file or secret manager with restrictive permissions.
- [ ] Run worker agent as a dedicated non-root account with only the required KVM/device and image/runtime directory permissions.
- [ ] Verify /dev/kvm access, image ownership/read-only policy, and Cloud Hypervisor binary ownership/checksum.
- [ ] Confirm VMM socket ownership and mode after launch; confirm no TCP listener exposes the VMM API.
- [ ] Confirm host firewall/security-group rules from the deployed controller and an unrelated source.
- [ ] Confirm logs and error responses contain no tokens, private addresses that should be redacted, or guest secrets.
- [ ] Confirm graceful service shutdown reaps owned VMM processes and the documented teardown leaves no billable test resources.

## Worker configuration notes

worker.example.json is a template only. Replace every image path with a staged, verified artifact on the worker. The controller's workers entries are trusted endpoints; do not populate them from user input or heartbeat payloads. Set the worker controller_heartbeat_url to the controller's private heartbeat endpoint and keep the shared secret identical via environment configuration.

The example secure_cookies value is false to keep local HTTP development usable. Production-like deployments must set it to true behind HTTPS.
