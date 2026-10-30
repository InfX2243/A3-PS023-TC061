package webui

const vmInventoryTemplate = `
{{define "vm-inventory"}}<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="theme-color" content="#f4f6fb"><title>Virtual machines · Aiori Compute</title>
{{if .Refreshing}}<meta http-equiv="refresh" content="5">{{end}}
{{template "styles" .}}<script src="/assets/ui.js" defer></script>
</head>
<body class="app-body vm-inventory-body">
<a class="skip-link" href="#main-content">Skip to main content</a>
<div class="app-shell">
{{template "sidebar" .}}
<main class="main-area" id="main-content">
<header class="topbar">
<div class="breadcrumbs"><a href="/console">Workspace</a><span aria-hidden="true">/</span><strong>Virtual machines</strong></div>
<div class="top-actions"><span class="env-pill"><span class="env-dot"></span> VM INVENTORY</span><a class="btn btn-secondary btn-small" href="/vms">↻ Refresh</a></div>
</header>
<div class="content vm-inventory-content">
<div class="page-heading">
<div><div class="eyebrow">Compute resources</div><h1>Virtual machines</h1><p>Review every VM, its worker placement, allocated resources, observed state, and latest lifecycle operation.</p></div>
<a class="btn btn-primary" href="/vms/new"><span aria-hidden="true">＋</span> Prepare a VM</a>
</div>
{{if .Notice}}<div class="notice" role="status"><span class="notice-icon">✓</span><span>{{.Notice}}</span></div>{{end}}
{{if .Error}}<div class="notice error" role="alert"><span class="notice-icon">!</span><span>{{.Error}}</span></div>{{end}}
<section class="vm-inventory-summary" aria-label="Virtual machine inventory summary">
<div class="vm-summary-card"><span class="vm-summary-icon">▣</span><div><strong>{{len .VMs}}</strong><span>Total virtual machines</span></div></div>
<div class="vm-summary-card"><span class="vm-summary-icon vm-summary-running">●</span><div><strong>{{countVMState .VMs "running"}}</strong><span>Observed running</span></div></div>
<div class="vm-summary-card"><span class="vm-summary-icon vm-summary-pending">↻</span><div><strong>{{countActiveOperations .VMs}}</strong><span>Operations in progress</span></div></div>
</section>
<section class="section-card vm-inventory-card">
<div class="section-head"><div><div class="section-title">VM inventory</div><div class="section-subtitle">Live controller records · observed state is authoritative</div></div><span class="count-pill">{{len .VMs}} total</span></div>
{{if .VMs}}
<div class="table-wrap"><table><thead><tr><th>Virtual machine</th><th>Placement</th><th>Resources</th><th>Image</th><th>Observed state</th><th>Latest operation</th><th>Actions</th></tr></thead><tbody>
{{range .VMs}}
<tr>
<td><div class="primary-cell">{{.VM.Name}}</div><div class="sub-cell mono">{{.VM.ID}}</div></td>
<td><div class="primary-cell">{{if .VM.WorkerID}}{{.VM.WorkerID}}{{else}}Unassigned{{end}}</div><div class="sub-cell">Worker node</div></td>
<td><div class="primary-cell">{{.VM.VCPUs}} vCPU</div><div class="sub-cell">{{divMiB .VM.MemoryBytes}} MiB memory</div></td>
<td><span class="mono">{{.VM.ImageID}}</span></td>
<td><span class="status status-{{.VM.Observed}}">{{.VM.Observed}}</span></td>
<td>{{if .HasOperation}}<div class="primary-cell">{{.Operation.Type}}: {{.Operation.State}}</div><div class="sub-cell">Latest controller operation</div>{{else}}<span class="muted">No operation</span>{{end}}</td>
<td><div class="action-row">
{{if or (stateIs .VM.Observed "stopped") (stateIs .VM.Observed "defined")}}<form class="action-form" method="post" action="/vms/{{.VM.ID}}/start"><input type="hidden" name="csrf_token" value="{{$.CSRFToken}}"><button class="btn btn-secondary btn-small" type="submit">▶ Start</button></form>{{end}}
{{if stateIs .VM.Observed "running"}}<form class="action-form" method="post" action="/vms/{{.VM.ID}}/stop"><input type="hidden" name="csrf_token" value="{{$.CSRFToken}}"><button class="btn btn-danger btn-small" type="submit">■ Stop</button></form>{{end}}
{{if or (stateIs .VM.Observed "unknown") (stateIs .VM.Observed "starting") (stateIs .VM.Observed "stopping")}}<span class="muted">Reconciling</span>{{end}}
</div></td>
</tr>
{{end}}
</tbody></table></div>
{{else}}
<div class="vm-empty-state"><span class="vm-empty-icon">▣</span><h2>No virtual machines yet</h2><p>Your VM inventory is empty. Prepare a VM to submit your first compute request, then track placement and lifecycle state here.</p><a class="btn btn-primary" href="/vms/new">Prepare your first VM <span aria-hidden="true">→</span></a></div>
{{end}}
</section>
<footer class="page-footer"><span>Aiori Compute · AIORI Hackathon engineering preview</span><span>VM state reflects controller observations, not a direct hypervisor query.</span></footer>
</div>
</main>
</div>
</body></html>{{end}}
`
