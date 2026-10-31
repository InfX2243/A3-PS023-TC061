# Frontend controller simulation

This document describes the browser-only product demonstration in `frontend/`. It complements the repository's MVP requirements, design, deployment and task documentation.

## Product scope

The interface represents a controller for exactly two KVM worker nodes. It is not a generic cloud-provider console. Its primary operator journeys are:

1. Inspect controller and worker health.
2. Open either worker and review heartbeat, CPU, memory, storage, network, VM count and event logs.
3. Provision a VM using explicit worker eligibility and resource-capacity checks.
4. See the selected worker on the VM inventory.
5. Receive a clear rejection when no worker meets the requested constraints.
6. Operate VM lifecycle controls and review events.

## Simulation boundary

All frontend data is generated and managed in the browser. Session, preferences and VM inventory use browser-local state/storage. No request is sent to the Go controller, worker agents, AWS or a hypervisor. Displayed IP addresses, host metrics, heartbeat values, VM state and logs are illustrative.

The simulation exists so the team can validate operator workflows and product design without paying for a live two-node KVM deployment. It must not be presented as proof that a host is reachable or that a VM was created on real hardware.

## Placement policy represented in the UI

A worker is eligible for a new VM only when:

- Worker state is healthy.
- Agent/heartbeat state is current.
- Hypervisor capability is available.
- CPU after the requested allocation remains below the configured safety threshold.
- Memory after the requested allocation remains below the configured safety threshold.
- Storage after the requested allocation remains below the configured safety threshold.

The current demo ranks eligible workers by combined resource pressure and chooses the least-loaded candidate. If no candidate passes all checks, the modal displays a rejection reason and does not add a VM. The exact arithmetic and thresholds are demo policy, not a substitute for the controller's authoritative scheduler.

## Node health model

A production worker detail view should expose:

- Stable worker ID, management address, agent version and host identity.
- Last authenticated heartbeat and freshness status.
- Agent connectivity, KVM availability and hypervisor version.
- CPU model/topology, utilisation and load.
- Total, used and available memory.
- Storage pools, capacity, free space and I/O.
- Network interfaces, addresses, throughput, packet/error counters.
- VM inventory and guest state.
- Structured agent/controller logs and correlated operation history.

A reachable worker is not automatically healthy. Stale heartbeats, missing KVM, agent failures or exhausted capacity must affect scheduling eligibility. An offline worker's existing VM state is unknown until reconciled; the UI must not infer that those VMs have stopped.

## VM lifecycle model

The product model should represent operations as stateful requests:

`Requested → Validating → Scheduled → Dispatched → Creating → Running`

Failure must retain a reason and operation/event history. Start, stop, restart and delete operations should be idempotent where possible, authorized server-side and reconciled against worker-reported state. This simulation models the visual flow only.

## Frontend integration seam

Keep presentation components independent from the source of data. When a backend is connected, introduce a typed API adapter that supplies:

- Worker inventory and health snapshots.
- Per-worker time-series metrics and log queries.
- VM inventory, resource requests and placement result.
- VM lifecycle operation status.
- Alerts/events and acknowledgement state.

The UI should not call worker agents directly. It should call the controller API; the controller remains responsible for authorization, scheduling, state, worker communication and reconciliation. API route names shown in the public handbook are proposed integration seams unless confirmed by a matching handler in the Go source.

## Accessibility requirements

The UI should target WCAG 2.2 AA:

- Keyboard-operable navigation, dialogs, menus and tables.
- Visible focus indicators and a skip-to-content link.
- Programmatic labels for all inputs and icon-only controls.
- Semantic headings, landmarks and table headers.
- Status communicated with text, not color alone.
- Sufficient text and control contrast.
- Reduced-motion support and no essential information conveyed only by animation.
- Error feedback associated with the relevant form and announced to assistive technology.
- Responsive reflow without horizontal page scrolling at narrow widths.

Run keyboard-only checks and an automated accessibility scan during frontend verification. Automated checks do not replace manual screen-reader and contrast testing.
