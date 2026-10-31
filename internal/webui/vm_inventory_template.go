package webui

// vmInventoryTemplate provides the virtual-machine operations workspace.
const vmInventoryTemplate = `
{{define "vm-inventory"}}<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="theme-color" content="#071426"><title>Virtual machines · Aiori Compute</title>
{{if .Refreshing}}<meta http-equiv="refresh" content="5">{{end}}
{{template "styles" .}}<script src="/assets/ui.js" defer></script>
</head>
<body class="app-body vm-inventory-body">
<a class="skip-link" href="#main-content">Skip to main content</a>
<div class="app-shell vm-shell">
{{template "sidebar" .}}
<main class="main-area vm-main" id="main-content">
<header class="topbar vm-topbar">
<div class="breadcrumbs"><a href="/console">Workspace</a><span aria-hidden="true">/</span><strong>Virtual machines</strong></div>
<div class="top-actions"><span class="vm-snapshot"><i></i>{{if .Refreshing}}RECONCILING INVENTORY{{else}}CONTROLLER INVENTORY{{end}}</span><a class="btn btn-secondary btn-small vm-refresh" href="/vms"><span aria-hidden="true">↻</span> Refresh inventory</a></div>
</header>
<div class="content vm-content">
<div class="vm-page">
<section class="vm-hero">
<div><div class="eyebrow">Compute resources / workload operations</div><h1>Virtual machines</h1><p>A clear operational view of provisioned workloads, placement, allocated capacity, and lifecycle activity across your compute fleet.</p></div>
<a class="btn btn-primary vm-create-cta" href="/vms/new"><span aria-hidden="true">＋</span> Prepare a VM <span aria-hidden="true">→</span></a>
</section>
{{if .Notice}}<div class="notice vm-notice" role="status"><span aria-hidden="true">✓</span>{{.Notice}}</div>{{end}}
{{if .Error}}<div class="notice error vm-notice" role="alert"><span aria-hidden="true">!</span>{{.Error}}</div>{{end}}

<section class="vm-metrics" aria-label="Virtual machine summary">
<article class="vm-metric"><div class="vm-metric-head"><span class="vm-metric-icon">▦</span><span>Registered workloads</span></div><strong>{{len .VMs}}</strong><small>Tracked by the controller</small></article>
<article class="vm-metric vm-metric-running"><div class="vm-metric-head"><span class="vm-metric-icon">●</span><span>Running</span></div><strong>{{countVMState .VMs "running"}}</strong><small>Observed active instances</small></article>
<article class="vm-metric vm-metric-stopped"><div class="vm-metric-head"><span class="vm-metric-icon">Ⅱ</span><span>Stopped</span></div><strong>{{countVMState .VMs "stopped"}}</strong><small>Available to start</small></article>
<article class="vm-metric vm-metric-activity"><div class="vm-metric-head"><span class="vm-metric-icon">↻</span><span>Active operations</span></div><strong>{{countActiveOperations .VMs}}</strong><small>Pending, dispatched, or reconciling</small></article>
</section>

<section class="vm-inventory-panel section-card">
<div class="vm-panel-heading">
<div><div class="vm-panel-kicker">RESOURCE REGISTER</div><h2>VM inventory</h2><p>Inspect workload configuration and current controller observations.</p></div>
<span class="vm-total-pill"><b>{{len .VMs}}</b> {{if eq (len .VMs) 1}}VM{{else}}VMs{{end}}</span>
</div>
<div class="vm-toolbar">
<label class="vm-search"><span aria-hidden="true">⌕</span><input type="search" placeholder="Search by name, ID, worker, or image…" aria-label="Search virtual machines" data-vm-search></label>
<div class="vm-toolbar-right"><label class="vm-filter-label" for="vm-state-filter">State</label><select id="vm-state-filter" data-vm-state-filter><option value="all">All states</option><option value="running">Running</option><option value="stopped">Stopped</option><option value="defined">Defined</option><option value="starting">Starting</option><option value="stopping">Stopping</option><option value="unknown">Unknown</option></select><span class="vm-visible-count" data-vm-visible-count>{{len .VMs}} shown</span></div>
</div>
{{if .VMs}}
<div class="vm-table-scroll" role="region" aria-label="Virtual machine inventory table" tabindex="0">
<table class="vm-table"><thead><tr><th scope="col">Virtual machine</th><th scope="col">Placement</th><th scope="col">Compute profile</th><th scope="col">Image</th><th scope="col">Observed state</th><th scope="col">Latest operation</th><th scope="col"><span class="sr-only">Actions</span></th></tr></thead>
<tbody>
{{range .VMs}}
<tr data-vm-row data-vm-state="{{.VM.Observed}}" data-search-text="{{.VM.Name}} {{.VM.ID}} {{.VM.WorkerID}} {{.VM.ImageID}} {{.VM.Observed}} {{if .HasOperation}}{{.Operation.Type}} {{.Operation.State}}{{end}}">
<td><div class="vm-name-cell"><span class="vm-machine-icon" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none"><rect x="3" y="4" width="18" height="13" rx="2"/><path d="M8 21h8M12 17v4M7 8h10M7 11h6"/></svg></span><span><strong>{{.VM.Name}}</strong><small>{{.VM.ID}}</small></span></div></td>
<td><div class="vm-placement">{{if .VM.WorkerID}}<span class="vm-worker-dot"></span>{{.VM.WorkerID}}{{else}}<span class="vm-unassigned">Unassigned</span>{{end}}</div><small class="vm-cell-hint">Worker node</small></td>
<td><div class="vm-profile"><strong>{{.VM.VCPUs}} <small>vCPU</small></strong><span>{{divMiB .VM.MemoryBytes}} MiB RAM</span></div></td>
<td><span class="vm-image-id">{{.VM.ImageID}}</span></td>
<td><span class="vm-state vm-state-{{.VM.Observed}}"><i></i>{{.VM.Observed}}</span></td>
<td>{{if .HasOperation}}<div class="vm-operation"><strong>{{.Operation.Type}}</strong><span>{{.Operation.State}}</span></div><small class="vm-cell-hint">Latest operation</small>{{else}}<span class="vm-no-operation">No recent activity</span>{{end}}</td>
<td><div class="vm-row-actions">
{{if or (stateIs .VM.Observed "stopped") (stateIs .VM.Observed "defined")}}<form class="action-form" method="post" action="/vms/{{.VM.ID}}/start"><input type="hidden" name="csrf_token" value="{{$.CSRFToken}}"><button class="vm-action vm-action-start" type="submit" aria-label="Start {{.VM.Name}}"><span aria-hidden="true">▶</span> Start</button></form>{{end}}
{{if stateIs .VM.Observed "running"}}<form class="action-form" method="post" action="/vms/{{.VM.ID}}/stop" data-confirm="Stop {{.VM.Name}}? The VM will no longer accept workload traffic until it is started again."><input type="hidden" name="csrf_token" value="{{$.CSRFToken}}"><button class="vm-action vm-action-stop" type="submit" aria-label="Stop {{.VM.Name}}"><span aria-hidden="true">■</span> Stop</button></form>{{end}}
{{if or (stateIs .VM.Observed "unknown") (stateIs .VM.Observed "starting") (stateIs .VM.Observed "stopping")}}<span class="vm-reconciling"><i></i> Syncing</span>{{end}}
</div></td>
</tr>
{{end}}
<tr data-vm-empty-filter hidden><td colspan="7"><div class="vm-no-results"><strong>No matching virtual machines</strong><span>Try a different search term or state filter.</span></div></td></tr>
</tbody></table>
</div>
<div class="vm-table-footer"><span>Showing controller-registered virtual machines</span><span>State is based on the latest controller observation</span></div>
{{else}}
<div class="vm-empty-state"><span class="vm-empty-art" aria-hidden="true"><svg viewBox="0 0 48 48" fill="none"><rect x="7" y="8" width="34" height="25" rx="4"/><path d="M17 40h14M24 33v7M13 15h22M13 20h13"/></svg></span><div class="vm-empty-kicker">NOTHING PROVISIONED YET</div><h3>Your compute workspace is ready</h3><p>Prepare your first virtual machine to submit a workload request. Once registered, its worker placement, resource profile, state, and lifecycle operations will appear here.</p><a class="btn btn-primary" href="/vms/new">Prepare your first VM <span aria-hidden="true">→</span></a></div>
{{end}}
</section>

<div class="vm-bottom-grid">
<section class="vm-guidance-card"><span class="vm-guidance-icon">i</span><div><strong>How to read VM state</strong><p>Observed state reflects the controller's latest report. Starting, stopping, and unknown states may indicate an operation is still being reconciled.</p></div><a href="/docs">View documentation <span aria-hidden="true">→</span></a></section>
<section class="vm-guidance-card vm-guidance-secondary"><span class="vm-guidance-icon">⌘</span><div><strong>Need more compute capacity?</strong><p>Review your worker fleet health and available memory before preparing additional workloads.</p></div><a href="/workers">View workers <span aria-hidden="true">→</span></a></section>
</div>
<footer class="vm-page-footer"><span>Aiori Compute · AIORI Hackathon engineering preview</span><span>Lifecycle actions are submitted to the controller and reconciled asynchronously.</span></footer>
</div>
</div>
</main>
</div>
</body></html>{{end}}
`
