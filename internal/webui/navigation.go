package webui

// sharedNavTemplate is the single source of truth for the authenticated console
// navigation. Every operational page renders this component.
const sharedNavTemplate = `
{{define "sidebar"}}<aside class="sidebar">
<a class="brand" href="/console"><span class="brand-mark">A</span><span><strong>Aiori Compute</strong><small>Control plane</small></span></a>
<nav class="sidebar-nav" aria-label="Main navigation">
<div class="nav-label">WORKSPACE</div>
<a class="nav-link" href="/console"><span class="nav-icon">▦</span><span class="nav-text">Overview</span></a>
<a class="nav-link" href="/workers"><span class="nav-icon">▤</span><span class="nav-text">Worker inventory</span></a>
<a class="nav-link" href="/console#vms"><span class="nav-icon">▣</span><span class="nav-text">Virtual machines</span></a>
<a class="nav-link" href="/vms/new"><span class="nav-icon">＋</span><span class="nav-text">Prepare a VM</span></a>
<a class="nav-link" href="/aws"><span class="nav-icon">☁</span><span class="nav-text">AWS EC2</span></a>
<a class="nav-link" href="/docs"><span class="nav-icon">?</span><span class="nav-text">Documentation</span></a>
</nav>
<div class="sidebar-spacer"></div>
<form method="post" action="/logout" class="sidebar-logout"><input type="hidden" name="csrf_token" value="{{.CSRFToken}}"><button class="btn btn-secondary" type="submit">Sign out</button></form>
</aside>{{end}}
`
