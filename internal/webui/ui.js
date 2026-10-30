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

  function initFormFeedback() {
    document.querySelectorAll('form[method="post"], form[method="POST"]').forEach(function (form) {
      form.addEventListener("submit", function () {
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
      var active = (path === "/console" && (href === "/console" || href === "/console#vms")) ||
        (path === "/workers" && href === "/workers") ||
        (path === "/aws" && href === "/aws") ||
        (path === "/vms/new" && href === "/vms/new") ||
        (path === "/docs" && href === "/docs");
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

  document.addEventListener("DOMContentLoaded", function () {
    initDocsNavigation();
    initFormFeedback();
    initWorkspaceNavigation();
    initLandingNavigation();
    initLandingPreviewTabs();
    initLandingMotion();
  });
}());
