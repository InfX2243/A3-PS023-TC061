package webui

// workerInventoryTemplate provides the worker fleet control surface.
const workerInventoryTemplate = `
{{define "workers"}}<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="theme-color" content="#071426"><title>Worker inventory · Aiori Compute</title>
{{template "styles" .}}<script src="/assets/ui.js" defer></script>
</head>
<body class="app-body worker-inventory-body">
<a class="skip-link" href="#main-content">Skip to main content</a>
<div class="app-shell worker-shell">
{{template "sidebar" .}}
<main class="main-area worker-main" id="main-content">
<header class="topbar worker-topbar">
<div class="breadcrumbs"><a href="/console">Workspace</a><span aria-hidden="true">/</span><strong>Worker inventory</strong></div>
<div class="top-actions"><span class="env-pill"><span class="env-dot"></span> LIVE AGENT HEALTH</span><a class="btn btn-secondary btn-small worker-refresh" href="/workers"><span aria-hidden="true">↻</span> Refresh inventory</a></div>
</header>
<div class="content worker-content">
<div class="worker-page">
<section class="worker-hero">
<div><div class="eyebrow">Compute fleet / infrastructure</div><h1>Worker inventory</h1><p>Monitor enrolled compute nodes, heartbeat freshness, hypervisor readiness, and available memory from one operational view.</p></div>
<div class="worker-hero-meta"><span class="worker-live-indicator"><i></i> Controller snapshot</span><span class="worker-hero-caption">Refresh to fetch the latest heartbeat and capacity data</span></div>
</section>
{{if .Notice}}<div class="notice worker-notice" role="status"><span aria-hidden="true">✓</span>{{.Notice}}</div>{{end}}
{{if .Error}}<div class="notice error worker-notice" role="alert"><span aria-hidden="true">!</span>{{.Error}}</div>{{end}}

<section class="worker-metrics" aria-label="Worker fleet summary">
<article class="worker-metric"><div class="worker-metric-top"><span class="worker-metric-icon">▤</span><span class="worker-metric-label">Registered nodes</span></div><strong>{{len .Workers}}</strong><span class="worker-metric-foot">Known to the controller</span></article>
<article class="worker-metric worker-metric-good"><div class="worker-metric-top"><span class="worker-metric-icon">✓</span><span class="worker-metric-label">Healthy</span></div><strong>{{countWorkerState .Workers "healthy"}}</strong><span class="worker-metric-foot">Heartbeat and hypervisor ready</span></article>
<article class="worker-metric worker-metric-warn"><div class="worker-metric-top"><span class="worker-metric-icon">◷</span><span class="worker-metric-label">Stale</span></div><strong>{{countWorkerState .Workers "stale"}}</strong><span class="worker-metric-foot">Heartbeat is overdue</span></article>
<article class="worker-metric worker-metric-bad"><div class="worker-metric-top"><span class="worker-metric-icon">!</span><span class="worker-metric-label">Unavailable</span></div><strong>{{countWorkerState .Workers "unavailable"}}</strong><span class="worker-metric-foot">Not ready to host workloads</span></article>
</section>

<div class="worker-section-heading"><div><h2>Fleet overview</h2><p>Inspect node health and available capacity. Select a row action to remove an obsolete registration.</p></div><span class="worker-record-count">{{len .Workers}} records</span></div>
<div class="worker-grid">
<section class="section-card worker-inventory-card">
<div class="section-head worker-card-head"><div><h3 class="section-title">Registered workers</h3><div class="section-subtitle">Authenticated agent registrations and most recent heartbeat details</div></div>
<div class="worker-table-tools"><label class="worker-search"><span aria-hidden="true">⌕</span><input type="search" placeholder="Search workers…" aria-label="Search workers" data-worker-search></label><span class="worker-count-pill">{{len .Workers}} nodes</span></div></div>
{{if .Workers}}
<div class="worker-table-scroll" role="region" aria-label="Worker inventory table" tabindex="0">
<table class="worker-table"><thead><tr><th scope="col">Worker</th><th scope="col">Health</th><th scope="col">Last heartbeat</th><th scope="col">Memory headroom</th><th scope="col">Agent endpoint</th><th scope="col"><span class="sr-only">Actions</span></th></tr></thead>
<tbody data-worker-rows>
{{range .Workers}}
<tr data-worker-row data-search-text="{{.Name}} {{.ID}} {{.Endpoint}} {{.State}}">
<td><div class="worker-node-cell"><span class="worker-node-avatar" aria-hidden="true">▤</span><span><strong>{{.Name}}</strong><small class="worker-id">{{.ID}}</small></span></div></td>
<td><span class="worker-state {{.State}}"><i></i>{{.State}}</span></td>
<td><span class="worker-heartbeat">{{if .LastHeartbeat.IsZero}}Never received{{else}}{{.LastHeartbeat.Format "02 Jan 2006 · 15:04:05 UTC"}}{{end}}</span><small class="worker-muted">{{if .LastHeartbeat.IsZero}}Awaiting first heartbeat{{else}}Last authenticated check-in{{end}}</small></td>
<td><div class="worker-memory-value"><strong>{{divMiB .AvailableMemoryBytes}} MiB</strong><span>of {{divMiB .MemoryLimitBytes}} MiB</span></div><div class="worker-memory-track" role="progressbar" aria-label="Available memory" aria-valuemin="0" aria-valuemax="100" aria-valuenow="{{memoryPercent .AvailableMemoryBytes .MemoryLimitBytes}}"><div class="worker-memory-fill" style="width:{{memoryPercent .AvailableMemoryBytes .MemoryLimitBytes}}%"></div></div></td>
<td><span class="worker-endpoint">{{.Endpoint}}</span></td>
<td><form method="post" action="/workers/{{.ID}}/delete" data-confirm="Remove this worker registration? This does not stop or terminate the EC2 instance."><input type="hidden" name="csrf_token" value="{{$.CSRFToken}}"><button class="worker-icon-button" type="submit" aria-label="Deregister {{.Name}}" title="Deregister worker">↗</button></form></td>
</tr>
{{end}}
<tr data-worker-empty-filter hidden><td colspan="6"><div class="worker-no-results">No workers match that search. Try another name, ID, endpoint, or status.</div></td></tr>
</tbody></table>
</div>
<p class="worker-table-footnote"><span>Live controller records</span><span>Memory shows currently available bytes against the registered cap</span></p>
{{else}}
<div class="worker-empty-state"><span class="worker-empty-art" aria-hidden="true"><svg viewBox="0 0 48 48" fill="none"><rect x="8" y="7" width="32" height="13" rx="3"/><rect x="8" y="28" width="32" height="13" rx="3"/><path d="M14 13.5h.01M14 34.5h.01M21 13.5h12M21 34.5h12"/></svg></span><h3>Your fleet is ready for its first worker</h3><p>Launch an EC2 instance with worker bootstrap enabled, or register an existing agent manually. A node appears after the controller receives its first authenticated heartbeat.</p><a class="btn btn-primary" href="/aws">Provision a worker <span aria-hidden="true">→</span></a></div>
{{end}}
</section>

<aside class="worker-side-column">
<section class="section-card worker-enrollment-card"><div class="worker-card-kicker"><span class="worker-card-kicker-icon">✦</span> RECOMMENDED WORKFLOW</div><h3>Automatic enrollment</h3><p class="worker-side-lede">Let the controller discover and register your EC2 workers as they come online.</p>
<ol class="worker-steps"><li><span>01</span><div><strong>Launch with bootstrap</strong><small>Use the managed EC2 launch flow and worker instance profile.</small></div></li><li><span>02</span><div><strong>Agent checks in</strong><small>The agent sends an authenticated heartbeat with its instance identity.</small></div></li><li><span>03</span><div><strong>Verify readiness</strong><small>Cloud Hypervisor and /dev/kvm probes determine worker health.</small></div></li></ol>
<a class="worker-side-link" href="/aws">Manage AWS EC2 instances <span aria-hidden="true">→</span></a>
</section>
<section class="worker-health-note"><span class="worker-health-note-icon">i</span><div><strong>Understand worker health</strong><p>A running EC2 instance is not automatically a healthy worker. Health reflects authenticated heartbeat freshness and successful hypervisor readiness checks.</p><a href="/docs">Read worker documentation →</a></div></section>
</aside>
</div>

<section class="section-card worker-register-card"><div class="worker-register-heading"><div><div class="eyebrow">Manual onboarding</div><h2>Register an existing worker</h2><p>For agents that are not created through the managed EC2 bootstrap workflow.</p></div><span class="worker-register-badge">ADVANCED</span></div>
<form method="post" action="/workers" class="worker-register-form"><input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
<div class="field"><label for="worker-id">Worker ID</label><input id="worker-id" name="id" required maxlength="128" pattern="[A-Za-z0-9][A-Za-z0-9._-]*" placeholder="worker-a"><small class="field-hint">Unique identifier using letters, numbers, dots, underscores, or hyphens.</small></div>
<div class="field"><label for="worker-name">Display name</label><input id="worker-name" name="name" required maxlength="128" placeholder="Worker A"><small class="field-hint">A recognizable name for the console.</small></div>
<div class="field"><label for="worker-endpoint">Agent endpoint</label><input id="worker-endpoint" name="endpoint" type="url" required placeholder="http://10.0.1.12:9090"><small class="field-hint">Absolute HTTP(S) URL reachable by the controller.</small></div>
<div class="field"><label for="worker-memory">Memory cap (MiB)</label><input id="worker-memory" name="memory_limit_mib" type="number" min="1" max="1048576" required value="4096"><small class="field-hint">Positive memory limit advertised for this worker.</small></div>
<div class="worker-register-submit"><button class="btn btn-primary" type="submit">Register worker <span aria-hidden="true">→</span></button><span>Registration is protected by your operator session and CSRF token.</span></div>
</form></section>
<footer class="worker-page-footer"><span>Aiori Compute · AIORI Hackathon engineering preview</span><span>Worker readiness is based on the latest authenticated controller heartbeat.</span></footer>
</div>
</div>
</main>
</div>
</body></html>{{end}}`
