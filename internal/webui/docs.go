package webui

import "net/http"

// docsPage is intentionally public: operators and reviewers can understand the
// platform before they authenticate to the control plane.
func (h *Handler) docsPage(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	h.render(w, "docs", nil)
}

const docsPageTemplate = `
{{define "docs"}}<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="theme-color" content="#11192e">
<meta name="description" content="Aiori Compute operator guide: architecture, setup, VM lifecycle, worker health, security, and troubleshooting.">
<title>Documentation · Aiori Compute</title>
{{template "styles" .}}
<style>
.docs-page{min-height:100vh;background:#f7f8fc;color:#20283a}
.docs-topbar{height:68px;display:flex;align-items:center;justify-content:space-between;gap:20px;padding:0 max(24px,calc((100vw - 1320px)/2));background:#fff;border-bottom:1px solid #e4e8f0;position:sticky;top:0;z-index:5}
.docs-brand{display:flex;align-items:center;gap:11px;color:#20283a;text-decoration:none}
.docs-brand .brand-mark{background:#252f56;color:#fff}
.docs-brand-name{font-size:14px;font-weight:800;letter-spacing:-.3px}
.docs-brand-sub{font-size:10px;color:#536078;margin-top:2px}
.docs-top-links{display:flex;align-items:center;gap:10px}
.docs-top-links a{font-size:12px;text-decoration:none;color:#536078;font-weight:650}
.docs-top-links a:hover{color:#5556d5}
.docs-hero{background:radial-gradient(ellipse at 88% 15%,#474a9a 0%,transparent 34%),linear-gradient(120deg,#11192e,#252f56 72%,#343b76);color:#fff;padding:54px max(24px,calc((100vw - 1160px)/2)) 48px}
.docs-hero-inner{max-width:850px}
.docs-kicker{color:#b9c0ff;font-size:10px;letter-spacing:1.8px;text-transform:uppercase;font-weight:800}
.docs-hero h1{font-size:clamp(30px,4vw,45px);line-height:1.1;letter-spacing:-1.7px;margin:13px 0 13px}
.docs-hero p{max-width:730px;color:#d0d7ed;font-size:14px;line-height:1.8;margin:0}
.docs-hero-meta{display:flex;flex-wrap:wrap;gap:10px;margin-top:22px}
.docs-chip{border:1px solid #ffffff2b;border-radius:7px;padding:7px 10px;color:#e0e5f8;font-size:10px}
.docs-layout{max-width:1320px;margin:0 auto;padding:32px 24px 70px;display:grid;grid-template-columns:220px minmax(0,780px) 180px;gap:36px;align-items:start}
.docs-sidebar{position:sticky;top:94px;max-height:calc(100vh - 120px);overflow:auto;padding:5px 0 20px}
.docs-nav-label{font-size:9px;text-transform:uppercase;letter-spacing:1.4px;font-weight:800;color:#58647a;margin:15px 0 8px}
.docs-nav{display:grid;gap:3px}
.docs-nav a{font-size:11px;color:#657087;text-decoration:none;padding:8px 10px;border-radius:7px;line-height:1.35}
.docs-nav a:hover{background:#eceeff;color:#4e50c5}
.docs-nav a.docs-nav-primary{color:#4e50c5;font-weight:750}
.docs-article{min-width:0}
.docs-section{padding:0 0 30px;margin:0 0 29px;border-bottom:1px solid #e4e8f0;scroll-margin-top:92px}
.docs-section:last-child{border-bottom:0}
.docs-section h2{font-size:22px;letter-spacing:-.6px;margin:0 0 12px;color:#20283a}
.docs-section h3{font-size:14px;margin:23px 0 8px;letter-spacing:-.2px}
.docs-section p,.docs-section li{font-size:12px;line-height:1.85;color:#58647a}
.docs-section p{margin:9px 0}
.docs-section ul,.docs-section ol{padding-left:20px;margin:9px 0}
.docs-lead{font-size:14px!important;color:#536078!important;line-height:1.85!important}
.docs-note{padding:13px 15px;border:1px solid #dfe3fa;background:#f0f1ff;border-radius:9px;color:#4f529f;font-size:11px;line-height:1.75;margin:15px 0}
.docs-note.warning{border-color:#f0dfb7;background:#fff8e8;color:#875e17}
.docs-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:11px;margin:16px 0}
.docs-card{background:#fff;border:1px solid #e3e7f0;border-radius:10px;padding:15px}
.docs-card h3{font-size:12px;margin:0 0 6px}
.docs-card p{font-size:11px;line-height:1.7;margin:0}
.docs-card a{font-size:11px;color:#5355cb;text-decoration:none;font-weight:750;display:inline-block;margin-top:10px}
.docs-card a:hover{text-decoration:underline}
.docs-table-wrap{overflow-x:auto;border:1px solid #e4e8f0;border-radius:9px;background:#fff;margin:13px 0}
.docs-table{border-collapse:collapse;width:100%;font-size:11px;white-space:normal}
.docs-table th{text-align:left;background:#f9faff;color:#58647a;font-size:9px;letter-spacing:.7px;text-transform:uppercase;padding:10px 12px}
.docs-table td{padding:11px 12px;border-top:1px solid #edf0f5;line-height:1.65;color:#58647a;vertical-align:top}
.docs-table td:first-child{font-weight:750;color:#303b51;white-space:nowrap}
.docs-code{position:relative;background:#151d31;color:#e2e8f8;border-radius:9px;padding:15px 16px;margin:12px 0;overflow-x:auto}
.docs-code pre{margin:0;white-space:pre;line-height:1.75;font-size:11px;font-family:ui-monospace,SFMono-Regular,Menlo,monospace}
.docs-inline-code{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:10px;background:#eceef5;color:#3e4960;padding:2px 4px;border-radius:4px}
.docs-aside{position:sticky;top:94px}
.docs-aside-card{background:#fff;border:1px solid #e3e7f0;border-radius:10px;padding:14px}
.docs-aside-title{font-size:10px;text-transform:uppercase;letter-spacing:1px;color:#58647a;font-weight:800;margin-bottom:11px}
.docs-aside a{display:block;font-size:10px;line-height:1.5;color:#5d6880;text-decoration:none;margin:10px 0}
.docs-aside a:hover{color:#5355cb}
.docs-aside .docs-aside-cta{border-top:1px solid #edf0f5;padding-top:12px;margin-top:13px;color:#5355cb;font-weight:800}
.docs-footer{max-width:1320px;margin:0 auto;padding:20px 24px 28px;border-top:1px solid #e4e8f0;display:flex;justify-content:space-between;gap:12px;color:#58647a;font-size:10px}
.docs-footer a{color:#5b5dd2;text-decoration:none}
@media(max-width:1050px){.docs-layout{grid-template-columns:190px minmax(0,1fr);gap:25px}.docs-aside{display:none}}
@media(max-width:680px){.docs-topbar{height:60px;padding:0 16px}.docs-top-links .docs-top-secondary{display:none}.docs-hero{padding:37px 20px 34px}.docs-layout{display:block;padding:27px 18px 45px}.docs-sidebar{position:static;max-height:none;border:1px solid #e3e7f0;background:#fff;border-radius:10px;padding:12px;margin-bottom:28px}.docs-nav{grid-template-columns:repeat(2,minmax(0,1fr))}.docs-nav-label{margin-top:8px}.docs-grid{grid-template-columns:1fr}.docs-footer{padding:18px;display:block}.docs-footer span{display:block;margin:5px 0}}
</style>
</head>
<body class="docs-page">
<a class="skip-link" href="#main-content">Skip to main content</a>
<header class="docs-topbar">
<a class="docs-brand" href="/" aria-label="Aiori Compute home"><span class="brand-mark">A</span><span><span class="docs-brand-name">Aiori Compute</span><span class="docs-brand-sub" style="display:block">Platform documentation</span></span></a>
<nav class="docs-top-links" aria-label="Documentation actions"><a class="docs-top-secondary" href="#getting-started">Quick start</a><a href="#architecture">Architecture</a><a class="btn btn-primary btn-small" href="/login">Operator sign in →</a></nav>
</header>
<section class="docs-hero"><div class="docs-hero-inner"><div class="docs-kicker">Operator guide · Engineering preview</div><h1>Understand the platform.<br>Operate with confidence.</h1><p>A practical guide to Aiori Compute: what the control plane does, how worker nodes and virtual machines fit together, how to create and operate a VM, and how to interpret states when an operation is still being reconciled.</p><div class="docs-hero-meta"><span class="docs-chip">Architecture and concepts</span><span class="docs-chip">Step-by-step operator workflow</span><span class="docs-chip">Security and troubleshooting</span></div></div></section>
<div class="docs-layout">
<aside class="docs-sidebar" aria-label="Documentation contents">
<div class="docs-nav-label">Start here</div><nav class="docs-nav"><a class="docs-nav-primary" href="#overview">Platform overview</a><a href="#getting-started">Quick start</a><a href="#architecture">Architecture</a></nav>
<div class="docs-nav-label">Operate</div><nav class="docs-nav"><a href="#workers">Worker fleet</a><a href="#virtual-machines">Virtual machines</a><a href="#lifecycle">Lifecycle and states</a><a href="#placement">Placement and capacity</a></nav>
<div class="docs-nav-label">Reference</div><nav class="docs-nav"><a href="#security">Security model</a><a href="#troubleshooting">Troubleshooting</a><a href="#current-limitations">Current limitations</a></nav>
</aside>
<main class="docs-article" id="main-content">
<section class="docs-section" id="overview"><h2>Platform overview</h2><p class="docs-lead">Aiori Compute is a small VM control plane built around a controller, worker agents, and Cloud Hypervisor. It provides one operator interface and API for requesting VM lifecycle changes across a worker fleet.</p>
<div class="docs-grid"><article class="docs-card"><h3>Control plane</h3><p>Validates requests, records inventory and operations, chooses a worker, and reconciles uncertain outcomes.</p><a href="#architecture">Explore architecture →</a></article><article class="docs-card"><h3>Worker agent</h3><p>Runs on a Linux host with KVM access and manages local VM processes and their disk/runtime resources.</p><a href="#workers">Worker requirements →</a></article><article class="docs-card"><h3>Operator interface</h3><p>Shows tracked worker and VM state, provides controlled lifecycle actions, and keeps uncertain operations visible.</p><a href="#virtual-machines">Using the console →</a></article><article class="docs-card"><h3>Documentation before sign-in</h3><p>This guide is public so teammates and reviewers can understand the product without an operator token.</p><a href="#getting-started">Start learning →</a></article></div>
<div class="docs-note">This is an engineering preview, not a claim of production readiness. A green page or a registered worker does not by itself prove that a real guest can boot on a particular host.</div></section>
<section class="docs-section" id="getting-started"><h2>Quick start</h2><p>Use this path to explore the interface locally. Real VM provisioning requires a configured, reachable worker with the required virtualization runtime and approved guest image artifacts.</p>
<h3>1. Start the controller</h3><p>From the repository root, configure a local operator token and start the controller with the example configuration.</p><div class="docs-code"><pre>export VM_CONTROLLER_OPERATOR_TOKEN='replace-with-a-local-development-token'
go run ./cmd/controller -config config.example.json</pre></div><p>The example configuration listens on <span class="docs-inline-code">127.0.0.1:8080</span>. Check the health endpoint separately:</p><div class="docs-code"><pre>curl http://127.0.0.1:8080/healthz</pre></div>
<h3>2. Open the console and read the guide</h3><p>Visit <span class="docs-inline-code">http://127.0.0.1:8080/</span> for the public landing page, <span class="docs-inline-code">http://127.0.0.1:8080/docs</span> for this guide, and <span class="docs-inline-code">http://127.0.0.1:8080/login</span> to sign in. The operator dashboard lives at <span class="docs-inline-code">/console</span>; dashboard and VM actions require an operator session.</p>
<h3>3. Connect a worker before creating a real VM</h3><p>Configure the worker agent using <span class="docs-inline-code">worker.example.json</span>, the deployment runbook, a shared secret, and a host with Linux/KVM and Cloud Hypervisor installed. The sample controller configuration with an empty worker list is useful for exploring the UI, but cannot place a VM.</p>
<div class="docs-note warning">Do not expose the development listener to the public internet. Use a strong secret, a protected network path, and HTTPS with secure cookies for a deployed operator interface.</div></section>
<section class="docs-section" id="architecture"><h2>Architecture and request flow</h2><p>The controller is the source of truth for tracked inventory and operation records. It does not directly run the guest process; it asks a selected worker agent to perform work and later reconciles the observed outcome.</p>
<div class="docs-table-wrap"><table class="docs-table"><thead><tr><th>Component</th><th>Responsibility</th><th>What it does not imply</th></tr></thead><tbody><tr><td>Operator UI</td><td>Displays inventory and submits authenticated, CSRF-protected actions.</td><td>A displayed state is not a direct hypervisor console.</td></tr><tr><td>Controller API and service</td><td>Validates input, persists VM/operation records, coordinates placement and worker requests.</td><td>A successful HTTP response alone does not prove a guest is running.</td></tr><tr><td>Placement</td><td>Filters candidates by reported health, freshness, memory and configured resource caps.</td><td>Capacity snapshots cannot guarantee the host will never change or fail.</td></tr><tr><td>Worker agent</td><td>Prepares per-VM runtime resources and invokes Cloud Hypervisor for lifecycle actions.</td><td>It is not a replacement for host isolation, patching, or access controls.</td></tr><tr><td>Cloud Hypervisor + KVM</td><td>Runs the VM process on a supported Linux host.</td><td>Availability depends on actual KVM access, artifacts, host kernel and runtime configuration.</td></tr></tbody></table></div>
<h3>Typical create request</h3><ol><li>The operator submits a VM name, vCPU count, memory, and an approved image identifier.</li><li>The controller validates limits and reserves suitable capacity through placement.</li><li>The controller records the request and dispatches it to a worker.</li><li>The worker prepares the VM resources and asks Cloud Hypervisor to create/start the guest.</li><li>The controller reconciles the response and records confirmed state; if the outcome is ambiguous, it remains visibly unknown until reconciliation can resolve it.</li></ol></section>
<section class="docs-section" id="workers"><h2>Worker fleet</h2><p>A worker is a host-side agent that reports its status and capacity and performs VM operations. A useful fleet view should answer: which worker is registered, whether its health is current, and how much memory is reported available.</p>
<h3>Worker prerequisites</h3><ul><li>A supported Linux host with accessible <span class="docs-inline-code">/dev/kvm</span>.</li><li>A compatible Cloud Hypervisor binary and guest kernel/disk artifacts.</li><li>Worker configuration with a unique identity, controller endpoint, and matching shared secret.</li><li>Network reachability to the controller and an appropriately restricted worker API.</li></ul>
<h3>Interpreting worker status</h3><p>Healthy means the controller currently considers the worker's health signal acceptable. Degraded or stale health should prompt a check of connectivity, heartbeat age, host pressure, and agent logs. Available memory is a reported placement signal, not a reservation of physical RAM for every future request.</p><p>For deployment and teardown procedures, use the repository's <span class="docs-inline-code">docs/deployment/worker-deployment.md</span> runbook and <span class="docs-inline-code">docs/deployment/aws-worker-validation.md</span> feasibility gate.</p></section>
<section class="docs-section" id="virtual-machines"><h2>Virtual machines</h2><p>Use <strong>Prepare a VM</strong> to submit a VM request. Only image identifiers configured in the controller's approved image list are accepted. The console intentionally avoids arbitrary image URLs or user-supplied host paths.</p>
<h3>Before submitting</h3><ul><li>Choose a descriptive name that identifies the workload.</li><li>Stay within configured vCPU and memory limits.</li><li>Use an approved image ID that corresponds to artifacts configured on the worker.</li><li>Confirm that at least one worker is healthy and has sufficient reported capacity.</li></ul>
<h3>Reading the VM table</h3><div class="docs-table-wrap"><table class="docs-table"><thead><tr><th>Field</th><th>Meaning</th></tr></thead><tbody><tr><td>Placement</td><td>The worker associated with the VM, or Unassigned if placement is not yet recorded.</td></tr><tr><td>Resources</td><td>The requested vCPU and memory values tracked for the VM.</td></tr><tr><td>Image</td><td>The configured image identifier, not necessarily a human-readable distribution name.</td></tr><tr><td>Observed state</td><td>The latest state the controller has recorded from worker responses/reconciliation.</td></tr><tr><td>Latest operation</td><td>The most recent tracked lifecycle operation and its current status.</td></tr></tbody></table></div>
<p>Lifecycle actions are shown only when the observed state supports them. If an operation is unresolved, the interface avoids presenting a misleading start/stop action that assumes the VM's state is known.</p></section>
<section class="docs-section" id="lifecycle"><h2>Lifecycle and operation states</h2><p>VM state and operation state are related but not interchangeable. VM state describes the latest observed condition of a VM; operation state describes progress and confidence in a specific request.</p>
<div class="docs-table-wrap"><table class="docs-table"><thead><tr><th>State or status</th><th>How to interpret it</th><th>Operator response</th></tr></thead><tbody><tr><td>Defined / stopped</td><td>VM is tracked and not currently observed as running.</td><td>Start is offered when the current state allows it.</td></tr><tr><td>Running</td><td>The controller has observed a running state.</td><td>Inspect workload health separately; use Stop only when appropriate.</td></tr><tr><td>Starting / stopping</td><td>A transition is in progress or being reconciled.</td><td>Wait for reconciliation and refresh the inventory.</td></tr><tr><td>Unknown</td><td>The controller cannot currently confirm the outcome or current VM state.</td><td>Do not blindly repeat actions; check worker connectivity and operation history.</td></tr><tr><td>Pending / dispatched</td><td>An operation is recorded or sent, but a final result has not yet been confirmed.</td><td>Allow reconciliation to run and inspect worker/controller logs if it persists.</td></tr><tr><td>Failed</td><td>An operation reported failure.</td><td>Read the controller and worker logs before retrying.</td></tr></tbody></table></div>
<div class="docs-note">Unknown is not the same as failed, and neither means success. Preserving uncertainty is deliberate: a network timeout can happen after the worker has already performed the requested action.</div></section>
<section class="docs-section" id="placement"><h2>Placement and capacity</h2><p>Placement uses the worker inventory available to the controller and considers health/freshness, available memory, requested resources, and configured limits. Capacity-aware selection reduces obvious overcommitment but is not a substitute for monitoring the host.</p><ul><li>Keep worker heartbeat and health reporting operational.</li><li>Size VM memory conservatively and leave host headroom for the VMM and operating system.</li><li>Investigate rejected placement requests by checking worker health, memory, configured caps, and image allowlists.</li><li>For multiple workers, validate that each worker can independently boot the approved image and has sufficient KVM/runtime permissions.</li></ul></section>
<section class="docs-section" id="security"><h2>Security model</h2><p>The operator UI requires an operator token at sign-in and uses a signed, expiring session cookie. Mutating forms carry a CSRF token. The API and worker communication have their own authentication controls; these layers should be deployed with network restrictions, not treated as a reason to expose management ports publicly.</p><ul><li>Use a long, unique operator token and worker shared secret. Never commit real credentials to the repository.</li><li>Keep the controller and worker API on private/restricted network paths.</li><li>Use HTTPS and enable secure cookies in deployed environments; the example configuration's HTTP-friendly setting is for local development only.</li><li>Apply least-privilege service accounts and restrict host access to VM images, runtime directories, sockets, and logs.</li><li>Review <span class="docs-inline-code">docs/security-checklist.md</span> before deployment. Code-level controls do not replace TLS, firewall, IAM, host hardening, or operational monitoring.</li></ul>
<div class="docs-note warning">This project is an MVP engineering build. Do not use it to host untrusted workloads or sensitive production data without a dedicated security review and additional isolation/operational controls.</div></section>
<section class="docs-section" id="troubleshooting"><h2>Troubleshooting</h2>
<h3>Dashboard redirects to sign in</h3><p>Sign in again with the configured operator token. Check that <span class="docs-inline-code">VM_CONTROLLER_OPERATOR_TOKEN</span> is set and that the controller was started with the expected configuration.</p>
<h3>VM request cannot be submitted</h3><p>Check the approved image list, vCPU/memory caps, worker health and available capacity. The UI intentionally returns a general message; consult controller logs for the operational cause.</p>
<h3>Worker is missing or unhealthy</h3><p>Check the worker service status, controller endpoint and network path, shared-secret agreement, heartbeat freshness, and host logs. Confirm the agent can access its runtime directory and KVM device.</p>
<h3>Operation stays unknown</h3><p>Do not assume that a timeout means the operation did not happen. Inspect controller and worker logs, restore connectivity, and allow reconciliation to query current status. A worker-agent restart may require manual investigation if the worker cannot reattach to previously running guests.</p>
<h3>Guest fails to boot</h3><p>Verify <span class="docs-inline-code">/dev/kvm</span>, Cloud Hypervisor compatibility, guest kernel and disk paths, permissions, available host resources, and the host's kernel/runtime logs. A successful build or health endpoint is not evidence of a successful guest boot.</p></section>
<section class="docs-section" id="current-limitations"><h2>Current limitations and validation status</h2><p>The interface and controller are under active MVP development. Treat these as explicit boundaries rather than assuming a feature has been validated simply because a screen exists.</p><ul><li>Real EC2/KVM guest boot and stop validation must be performed on the chosen host family before claiming runtime feasibility.</li><li>Two-worker placement and failover behavior require an actual multi-worker test.</li><li>Durable worker-side VM inventory and automatic reattachment after worker-agent restart are not yet complete.</li><li>Controller inventory and observed state may lag the real guest while a request is pending or a worker is unreachable.</li><li>Production deployment still requires HTTPS, network restrictions, host hardening, monitoring, backups, and a reviewed recovery procedure.</li></ul><p>For the authoritative implementation plan and deployment evidence, consult <span class="docs-inline-code">docs/tasks.md</span>, <span class="docs-inline-code">docs/demo-runbook.md</span>, and the deployment validation documents in the repository.</p><div class="docs-grid"><article class="docs-card"><h3>Ready to operate?</h3><p>Sign in to inspect current inventory and submit permitted VM actions.</p><a href="/login">Go to operator sign in →</a></article><article class="docs-card"><h3>Need deployment details?</h3><p>Use the source repository for runbooks, API contracts, and validation checklists.</p><a href="https://github.com/InfX2243/A3-PS023-TC061/tree/docs/mvp-requirements-design-task-plan/docs">Browse project documentation →</a></article></div></section>
</main>
<aside class="docs-aside"><div class="docs-aside-card"><div class="docs-aside-title">On this page</div><a href="#getting-started">Quick start</a><a href="#architecture">Request flow</a><a href="#virtual-machines">Create a VM</a><a href="#lifecycle">State reference</a><a href="#security">Security model</a><a href="#troubleshooting">Troubleshooting</a><a class="docs-aside-cta" href="/login">Open operator console →</a></div></aside>
</div>
<footer class="docs-footer"><span>Aiori Compute · Engineering preview</span><span>Documentation is public; infrastructure actions require operator authentication.</span><span><a href="/login">Operator sign in</a></span></footer>
</body></html>{{end}}
`;

