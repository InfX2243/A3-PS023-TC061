// Progressive enhancements for the server-rendered operator UI.
(function () {
  "use strict";

  function initDocsNavigation() {
    var sections = Array.prototype.slice.call(document.querySelectorAll(".docs-article .docs-section[id]"));
    var links = Array.prototype.slice.call(document.querySelectorAll(".docs-sidebar .docs-nav a[href^='#']"));
    if (!sections.length || !links.length) return;

    var byId = new Map();
    links.forEach(function (link) {
      var id = link.getAttribute("href").slice(1);
      if (!byId.has(id)) byId.set(id, []);
      byId.get(id).push(link);
      link.addEventListener("click", function () { setActive(id, true); });
    });

    function setActive(id, scrollIntoView) {
      links.forEach(function (link) {
        var active = link.getAttribute("href") === "#" + id;
        if (active) { link.classList.add("is-active"); link.setAttribute("aria-current", "location"); }
        else { link.classList.remove("is-active"); link.removeAttribute("aria-current"); }
      });
      var activeLink = byId.get(id) && byId.get(id)[0];
      if (scrollIntoView && activeLink && window.matchMedia("(min-width: 681px)").matches) {
        activeLink.scrollIntoView({ block: "nearest", behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth" });
      }
    }

    var initial = window.location.hash.slice(1);
    if (byId.has(initial)) setActive(initial, false);
    else setActive(sections[0].id, false);

    if ("IntersectionObserver" in window) {
      var visible = new Map();
      var observer = new IntersectionObserver(function (entries) {
        entries.forEach(function (entry) { visible.set(entry.target.id, entry.isIntersecting ? entry.intersectionRatio : 0); });
        var candidates = sections.filter(function (section) { return (visible.get(section.id) || 0) > 0; });
        if (candidates.length) {
          candidates.sort(function (a, b) {
            var aTop = Math.abs(a.getBoundingClientRect().top - 112);
            var bTop = Math.abs(b.getBoundingClientRect().top - 112);
            return aTop - bTop;
          });
          setActive(candidates[0].id, false);
        }
      }, { rootMargin: "-105px 0px -62% 0px", threshold: [0, 0.1, 0.25, 0.5] });
      sections.forEach(function (section) { observer.observe(section); });
    } else {
      window.addEventListener("scroll", function () {
        var current = sections[0];
        sections.forEach(function (section) { if (section.getBoundingClientRect().top <= 130) current = section; });
        setActive(current.id, false);
      }, { passive: true });
    }

    window.addEventListener("hashchange", function () {
      var id = window.location.hash.slice(1);
      if (byId.has(id)) setActive(id, true);
    });
  }

  function initWorkerInventoryInteractions() {
    var search = document.querySelector("[data-worker-search]");
    var rows = Array.prototype.slice.call(document.querySelectorAll("[data-worker-row]"));
    var empty = document.querySelector("[data-worker-empty-filter]");
    if (search && rows.length) {
      search.addEventListener("input", function () {
        var query = search.value.trim().toLowerCase();
        var visible = 0;
        rows.forEach(function (row) {
          var match = (row.getAttribute("data-search-text") || row.textContent).toLowerCase().indexOf(query) !== -1;
          row.hidden = !match;
          if (match) visible++;
        });
        if (empty) empty.hidden = visible !== 0;
      });
    }
  }

  function initVMInventoryInteractions() {
    var search = document.querySelector("[data-vm-search]");
    var stateFilter = document.querySelector("[data-vm-state-filter]");
    var rows = Array.prototype.slice.call(document.querySelectorAll("[data-vm-row]"));
    var empty = document.querySelector("[data-vm-empty-filter]");
    var count = document.querySelector("[data-vm-visible-count]");
    if (!rows.length) return;

    function applyFilters() {
      var query = search ? search.value.trim().toLowerCase() : "";
      var state = stateFilter ? stateFilter.value : "all";
      var visible = 0;
      rows.forEach(function (row) {
        var text = (row.getAttribute("data-search-text") || row.textContent).toLowerCase();
        var rowState = (row.getAttribute("data-vm-state") || "").toLowerCase();
        var matches = (!query || text.indexOf(query) !== -1) && (state === "all" || rowState === state);
        row.hidden = !matches;
        if (matches) visible++;
      });
      if (empty) empty.hidden = visible !== 0;
      if (count) count.textContent = visible + (visible === 1 ? " shown" : " shown");
    }

    if (search) search.addEventListener("input", applyFilters);
    if (stateFilter) stateFilter.addEventListener("change", applyFilters);
    applyFilters();
  }

  function initPrepareVMInteractions() {
    var form = document.querySelector("[data-vm-config-form]");
    if (!form) return;
    var name = form.querySelector("#name");
    var cpu = form.querySelector("#vcpus");
    var memory = form.querySelector("#memory_mib");
    var image = form.querySelector("#image_id");
    var previewName = document.querySelector("[data-vm-preview-name]");
    var previewCPU = document.querySelector("[data-vm-preview-vcpus]");
    var previewMemory = document.querySelector("[data-vm-preview-memory]");
    var previewImage = document.querySelector("[data-vm-preview-image]");
    var previewPercent = document.querySelector("[data-vm-preview-memory-percent]");
    var meter = document.querySelector("[data-vm-preview-meter]");
    var presets = Array.prototype.slice.call(document.querySelectorAll("[data-vm-preset]"));
    var maxMemory = memory ? Number(memory.max) : 0;

    function updatePreview() {
      var cpuValue = Math.max(0, Number(cpu && cpu.value) || 0);
      var memoryValue = Math.max(0, Number(memory && memory.value) || 0);
      if (previewName) previewName.textContent = (name && name.value.trim()) || "Untitled workload";
      if (previewCPU) previewCPU.textContent = String(cpuValue);
      if (previewMemory) previewMemory.textContent = (memoryValue / 1024).toLocaleString(undefined, {maximumFractionDigits: 2});
      if (previewImage) previewImage.textContent = image && image.value ? image.options[image.selectedIndex].text : "Not selected";
      var percent = maxMemory > 0 && memoryValue > 0 ? Math.min(100, memoryValue / maxMemory * 100) : 0;
      if (previewPercent) previewPercent.textContent = memoryValue > 0 ? Math.round(percent) + "%" : "—";
      if (meter) meter.style.width = percent + "%";
      presets.forEach(function (button) {
        var key = button.getAttribute("data-vm-preset");
        var match = key === "small" ? cpuValue === 1 && memoryValue === 1024 :
          key === "standard" ? cpuValue === 2 && memoryValue === 2048 :
          key === "compute" ? cpuValue === 4 && memoryValue === 4096 : false;
        button.classList.toggle("is-selected", match);
        button.setAttribute("aria-pressed", String(match));
      });
    }
    presets.forEach(function (button) {
      button.addEventListener("click", function () {
        var key = button.getAttribute("data-vm-preset");
        var values = key === "small" ? [1, 1024] : key === "standard" ? [2, 2048] : [4, 4096];
        if (cpu) cpu.value = String(Math.min(Number(cpu.max) || values[0], values[0]));
        if (memory) memory.value = String(Math.min(Number(memory.max) || values[1], values[1]));
        updatePreview();
      });
    });
    [name, cpu, memory, image].forEach(function (field) {
      if (!field) return;
      field.addEventListener("input", updatePreview);
      field.addEventListener("change", updatePreview);
    });
    form.addEventListener("submit", function (event) {
      [name, cpu, memory, image].forEach(function (field) {
        if (!field) return;
        var valid = field.checkValidity();
        field.setAttribute("aria-invalid", String(!valid));
      });
      if (!form.checkValidity()) {
        event.preventDefault();
        var invalid = form.querySelector(":invalid");
        if (invalid) invalid.focus();
      }
    });
    [name, cpu, memory, image].forEach(function (field) {
      if (!field) return;
      field.addEventListener("input", function () { field.removeAttribute("aria-invalid"); });
      field.addEventListener("change", function () { field.removeAttribute("aria-invalid"); });
    });
    updatePreview();
  }

  function initDashboardInteractions() {
    document.querySelectorAll("[data-alert-dismiss]").forEach(function (button) {
      button.addEventListener("click", function () {
        var alert = button.closest("[data-dashboard-alert]");
        if (!alert) return;
        alert.classList.add("is-dismissed");
        alert.setAttribute("aria-hidden", "true");
      });
    });
  }

  function initLoginInteractions() {
    document.querySelectorAll("[data-password-toggle]").forEach(function (toggle) {
      var input = document.getElementById(toggle.getAttribute("data-password-toggle"));
      if (!input) return;
      toggle.addEventListener("click", function () {
        var reveal = input.type === "password";
        input.type = reveal ? "text" : "password";
        toggle.textContent = reveal ? "Hide" : "Show";
        toggle.setAttribute("aria-pressed", String(reveal));
        toggle.setAttribute("aria-label", (reveal ? "Hide " : "Show ") + (input.id === "token" ? "access token" : "password"));
        input.focus();
      });
    });
  }

  function initFormFeedback() {
    document.querySelectorAll('form[method="post"], form[method="POST"]').forEach(function (form) {
      form.addEventListener("submit", function (event) {
        var confirmation = form.getAttribute("data-confirm");
        if (confirmation && !window.confirm(confirmation)) {
          event.preventDefault();
          return;
        }
        if (form.dataset.submitting === "true") return;
        form.dataset.submitting = "true";
        var button = form.querySelector('button[type="submit"],button:not([type])');
        if (!button) return;
        var action = (form.getAttribute("action") || "").toLowerCase();
        var label = action === "/login" ? "Signing in…" : action === "/logout" ? "Signing out…" : action.indexOf("/start") !== -1 ? "Starting…" : action.indexOf("/stop") !== -1 ? "Stopping…" : action === "/vms" ? "Submitting request…" : "Working…";
        button.setAttribute("aria-busy", "true");
        button.dataset.originalMarkup = button.innerHTML;
        button.innerHTML = '<span class="ui-button-spinner" aria-hidden="true"></span><span>' + label + '</span>';
        button.disabled = true;
        button.classList.add("is-loading");
      });
    });
  }

  function initWorkspaceNavigation() {
    var path = window.location.pathname;
    document.querySelectorAll(".sidebar-nav .nav-link").forEach(function (link) {
      var href = link.getAttribute("href") || "";
      var active = path === href ||
        (path === "/console/" && href === "/console");
      if (active) { link.classList.add("is-active"); link.setAttribute("aria-current", "page"); }
      else { link.classList.remove("is-active"); link.removeAttribute("aria-current"); }
    });
  }

  function initLandingNavigation() {
    var toggle = document.querySelector(".landing-menu-toggle");
    var nav = document.getElementById("landing-navigation");
    if (!toggle || !nav) return;

    function closeMenu() {
      toggle.setAttribute("aria-expanded", "false");
      toggle.setAttribute("aria-label", "Open navigation");
      nav.classList.remove("is-open");
    }
    toggle.addEventListener("click", function () {
      var expanded = toggle.getAttribute("aria-expanded") === "true";
      toggle.setAttribute("aria-expanded", String(!expanded));
      toggle.setAttribute("aria-label", expanded ? "Open navigation" : "Close navigation");
      nav.classList.toggle("is-open", !expanded);
    });
    nav.querySelectorAll("a").forEach(function (link) {
      link.addEventListener("click", closeMenu);
    });
    document.addEventListener("keydown", function (event) {
      if (event.key === "Escape") {
        closeMenu();
        toggle.focus();
      }
    });
    document.addEventListener("click", function (event) {
      if (!nav.contains(event.target) && !toggle.contains(event.target)) closeMenu();
    });
  }

  function initLandingPreviewTabs() {
    var tabs = Array.prototype.slice.call(document.querySelectorAll("[data-preview-tab]"));
    var panels = Array.prototype.slice.call(document.querySelectorAll("[data-preview-panel]"));
    if (!tabs.length || !panels.length) return;

    function activate(tab, moveFocus) {
      var name = tab.getAttribute("data-preview-tab");
      tabs.forEach(function (item) {
        var active = item === tab;
        item.classList.toggle("is-active", active);
        item.setAttribute("aria-selected", String(active));
        item.setAttribute("tabindex", active ? "0" : "-1");
      });
      panels.forEach(function (panel) {
        panel.hidden = panel.getAttribute("data-preview-panel") !== name;
      });
      if (moveFocus) tab.focus();
    }

    tabs.forEach(function (tab, index) {
      tab.setAttribute("tabindex", index === 0 ? "0" : "-1");
      tab.addEventListener("click", function () { activate(tab, false); });
      tab.addEventListener("keydown", function (event) {
        if (event.key !== "ArrowRight" && event.key !== "ArrowLeft" && event.key !== "Home" && event.key !== "End") return;
        event.preventDefault();
        var next = event.key === "Home" ? 0 : event.key === "End" ? tabs.length - 1 :
          (index + (event.key === "ArrowRight" ? 1 : tabs.length - 1)) % tabs.length;
        activate(tabs[next], true);
      });
    });
  }

  function initLandingMotion() {
    document.body.classList.add("landing-motion-ready");
    var revealItems = Array.prototype.slice.call(document.querySelectorAll("[data-reveal]"));
    var progress = document.querySelector(".landing-progress span");
    var reduceMotion = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;

    revealItems.forEach(function (item) {
      var delay = parseInt(item.getAttribute("data-reveal-delay") || "0", 10);
      item.style.setProperty("--reveal-delay", (isNaN(delay) ? 0 : Math.min(delay, 300)) + "ms");
    });

    if (reduceMotion || !("IntersectionObserver" in window)) {
      revealItems.forEach(function (item) { item.classList.add("is-visible"); });
    } else {
      var observer = new IntersectionObserver(function (entries) {
        entries.forEach(function (entry) {
          if (!entry.isIntersecting) return;
          entry.target.classList.add("is-visible");
          observer.unobserve(entry.target);
        });
      }, { threshold: 0.12, rootMargin: "0px 0px -35px 0px" });
      revealItems.forEach(function (item) { observer.observe(item); });
    }

    if (progress) {
      var ticking = false;
      function updateProgress() {
        var doc = document.documentElement;
        var max = doc.scrollHeight - window.innerHeight;
        var amount = max > 0 ? Math.max(0, Math.min(1, window.scrollY / max)) : 0;
        progress.style.width = (amount * 100) + "%";
        ticking = false;
      }
      window.addEventListener("scroll", function () {
        if (ticking) return;
        ticking = true;
        window.requestAnimationFrame(updateProgress);
      }, { passive: true });
      updateProgress();
    }
  }


  function initDocsInteractions() {
    var page = document.querySelector(".docs-page");
    if (!page) return;
    var sections = Array.prototype.slice.call(page.querySelectorAll(".docs-article .docs-section[id]"));
    var search = page.querySelector("#docs-search");
    var searchStatus = page.querySelector("#docs-search-status");
    var progress = page.querySelector(".docs-reading-progress span");
    var backTop = page.querySelector(".docs-back-top");
    var navGroups = Array.prototype.slice.call(page.querySelectorAll(".docs-sidebar > .docs-nav-label"));
    var navs = Array.prototype.slice.call(page.querySelectorAll(".docs-sidebar > .docs-nav"));
    var ticking = false;

    // Add accessible, no-dependency copy controls to each shell command/example.
    page.querySelectorAll(".docs-code").forEach(function (block) {
      var pre = block.querySelector("pre");
      if (!pre || block.querySelector(".docs-code-copy")) return;
      var button = document.createElement("button");
      button.type = "button";
      button.className = "docs-code-copy";
      button.textContent = "Copy";
      button.setAttribute("aria-label", "Copy code example");
      button.addEventListener("click", function () {
        var value = pre.innerText || pre.textContent || "";
        function copied() {
          button.textContent = "Copied";
          button.setAttribute("aria-label", "Code example copied");
          window.setTimeout(function () {
            button.textContent = "Copy";
            button.setAttribute("aria-label", "Copy code example");
          }, 1600);
        }
        if (navigator.clipboard && window.isSecureContext) {
          navigator.clipboard.writeText(value).then(copied).catch(function () {
            button.textContent = "Select to copy";
          });
        } else {
          var range = document.createRange();
          range.selectNodeContents(pre);
          var selection = window.getSelection();
          selection.removeAllRanges();
          selection.addRange(range);
          button.textContent = "Select to copy";
          button.setAttribute("aria-label", "Code selected; use your copy shortcut");
        }
      });
      block.appendChild(button);
    });

    // Give every section a stable deep link while preserving its existing heading.
    sections.forEach(function (section) {
      var heading = section.querySelector("h2");
      if (!heading || heading.querySelector(".docs-section-heading-link")) return;
      var anchor = document.createElement("a");
      anchor.className = "docs-section-heading-link";
      anchor.href = "#" + section.id;
      anchor.textContent = "#";
      anchor.setAttribute("aria-label", "Link to " + heading.textContent.trim());
      heading.appendChild(anchor);
    });

    function applySearch() {
      var query = (search ? search.value : "").trim().toLowerCase();
      var shown = 0;
      sections.forEach(function (section) {
        var match = !query || section.textContent.toLowerCase().indexOf(query) !== -1;
        section.hidden = !match;
        if (match) shown++;
        var links = page.querySelectorAll('.docs-sidebar a[href="#' + section.id + '"]');
        links.forEach(function (link) { link.hidden = !match; });
      });
      navGroups.forEach(function (label, index) {
        var nav = navs[index];
        if (!nav) return;
        label.hidden = !Array.prototype.some.call(nav.querySelectorAll("a"), function (link) { return !link.hidden; });
      });
      if (searchStatus) {
        searchStatus.textContent = query ? (shown ? shown + " matching sections" : "No matching sections") : sections.length + " topics · 1 guide";
      }
    }
    if (search) {
      search.addEventListener("input", applySearch);
      search.addEventListener("keydown", function (event) {
        if (event.key === "Escape" && search.value) {
          search.value = "";
          applySearch();
        }
      });
    }

    function updateScrollUI() {
      var root = document.documentElement;
      var max = root.scrollHeight - window.innerHeight;
      var amount = max > 0 ? Math.max(0, Math.min(1, window.scrollY / max)) : 0;
      if (progress) progress.style.width = (amount * 100) + "%";
      if (backTop) backTop.classList.toggle("is-visible", window.scrollY > 520);
      ticking = false;
    }
    window.addEventListener("scroll", function () {
      if (ticking) return;
      ticking = true;
      window.requestAnimationFrame(updateScrollUI);
    }, { passive: true });
    updateScrollUI();
  }


  function initAWSInventoryInteractions() {
    var page = document.querySelector(".aws-page");
    if (!page) return;
    var search = page.querySelector("[data-aws-search]");
    var filter = page.querySelector("[data-aws-state-filter]");
    var rows = Array.prototype.slice.call(page.querySelectorAll("[data-aws-row]"));
    var count = page.querySelector("[data-aws-visible-count]");
    var empty = page.querySelector("[data-aws-no-results]");
    function applyFilters() {
      var query = (search ? search.value : "").trim().toLowerCase();
      var state = filter ? filter.value : "all";
      var visible = 0;
      rows.forEach(function (row) {
        var show = (!query || row.textContent.toLowerCase().indexOf(query) !== -1) &&
          (state === "all" || row.getAttribute("data-aws-state") === state);
        row.hidden = !show;
        if (show) visible++;
      });
      if (count) count.textContent = String(visible);
      if (empty) empty.hidden = visible !== 0 || rows.length === 0;
    }
    if (search) search.addEventListener("input", applyFilters);
    if (filter) filter.addEventListener("change", applyFilters);
    page.addEventListener("submit", function (event) {
      var message = event.target && event.target.getAttribute && event.target.getAttribute("data-aws-confirm");
      if (message && !window.confirm(message)) event.preventDefault();
    });
    applyFilters();
  }

  document.addEventListener("DOMContentLoaded", function () {
    initDocsNavigation();
    initDocsInteractions();
    initFormFeedback();
    initWorkspaceNavigation();
    initLandingNavigation();
    initLandingPreviewTabs();
    initLandingMotion();
    initLoginInteractions();
    initDashboardInteractions();
    initPrepareVMInteractions();
    initWorkerInventoryInteractions();
    initVMInventoryInteractions();
    initAWSInventoryInteractions();
  });
}());

