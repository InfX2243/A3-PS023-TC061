package webui

// sharedNavTemplate is the single source of truth for the authenticated console
// navigation. Every operational page renders this component.
const sharedNavTemplate = `
{{define "sidebar"}}<aside class="sidebar">
<a class="brand" href="/console"><span class="brand-mark">A</span><span><strong>Aiori Compute</strong><small>Control plane</small></span></a>
<nav class="sidebar-nav" aria-label="Main navigation">
<div class="nav-label">WORKSPACE</div>
<a class="nav-link" href="/console" title="Overview"><span class="nav-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><rect x="3.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="13.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.5"/><rect x="13.5" y="13.5" width="7" height="7" rx="1.5"/></svg></span><span class="nav-text">Overview</span></a>
<a class="nav-link" href="/workers" title="Worker inventory"><span class="nav-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><rect x="3" y="4" width="18" height="6" rx="1.5"/><rect x="3" y="14" width="18" height="6" rx="1.5"/><path d="M7 7h.01M7 17h.01M11 7h6M11 17h6"/></svg></span><span class="nav-text">Worker inventory</span></a>
<a class="nav-link" href="/vms" title="Virtual machines"><span class="nav-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><rect x="3" y="3" width="8" height="8" rx="1.5"/><rect x="13" y="3" width="8" height="8" rx="1.5"/><rect x="3" y="13" width="8" height="8" rx="1.5"/><rect x="13" y="13" width="8" height="8" rx="1.5"/></svg></span><span class="nav-text">Virtual machines</span></a>
<a class="nav-link" href="/vms/new" title="Prepare a VM"><span class="nav-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M12 8v8M8 12h8"/></svg></span><span class="nav-text">Prepare a VM</span></a>
<a class="nav-link" href="/aws" title="AWS EC2"><span class="nav-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M7 18h10a4 4 0 0 0 .5-7.97A6 6 0 0 0 6 9.5 4.25 4.25 0 0 0 7 18Z"/></svg></span><span class="nav-text">AWS EC2</span></a>
<a class="nav-link" href="/docs" title="Documentation"><span class="nav-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M4 5.5A2.5 2.5 0 0 1 6.5 3H20v17H6.5A2.5 2.5 0 0 1 4 17.5z"/><path d="M4 17.5A2.5 2.5 0 0 1 6.5 15H20M9 7h7M9 10h7"/></svg></span><span class="nav-text">Documentation</span></a>
</nav>
<div class="sidebar-spacer"></div>
<form method="post" action="/logout" class="sidebar-logout"><input type="hidden" name="csrf_token" value="{{.CSRFToken}}"><button class="btn btn-secondary" type="submit">Sign out</button></form>
</aside>{{end}}
`
