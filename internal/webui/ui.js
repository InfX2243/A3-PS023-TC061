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

  document.addEventListener("DOMContentLoaded", function () { initDocsNavigation(); initFormFeedback(); });
}());
