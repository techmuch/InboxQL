// InboxQL redesign mockups — the only script: a light/dark switch for reviewing
// both themes. Mockups are otherwise static; nothing here talks to a server.
(function () {
  var root = document.documentElement;
  var saved = null;
  try { saved = localStorage.getItem('iql-mock-theme'); } catch (e) { /* storage blocked: follow the system */ }
  if (saved === 'light' || saved === 'dark') root.setAttribute('data-theme', saved);

  function current() {
    var t = root.getAttribute('data-theme');
    if (t) return t;
    return window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }

  document.addEventListener('DOMContentLoaded', function () {
    var buttons = document.querySelectorAll('[data-theme-toggle]');
    function label() {
      buttons.forEach(function (b) {
        var next = current() === 'dark' ? 'light' : 'dark';
        b.textContent = 'Show ' + next + ' theme';
        b.setAttribute('aria-label', 'Show the ' + next + ' theme');
      });
    }
    buttons.forEach(function (b) {
      b.addEventListener('click', function () {
        var next = current() === 'dark' ? 'light' : 'dark';
        root.setAttribute('data-theme', next);
        try { localStorage.setItem('iql-mock-theme', next); } catch (e) { /* fine */ }
        label();
      });
    });
    label();
  });
})();

// The evidence panel is resizable. The divider follows the WAI-ARIA window
// splitter pattern: a focusable separator whose value is the panel's width.
//   drag, or ← → (Shift for bigger steps) · Home/End narrowest/widest
//   Enter or "." hides and shows the panel · double-click resets
// The width is remembered in this browser only; it is a per-viewer convenience.
(function () {
  var DEFAULT = 352, MIN = 256, STEP = 16;
  function load(k) { try { return localStorage.getItem(k); } catch (e) { return null; } }
  function save(k, v) { try { localStorage.setItem(k, v); } catch (e) { /* not remembered: fine */ } }

  document.addEventListener('DOMContentLoaded', function () {
    var shell = document.querySelector('.shell');
    var panel = shell && shell.querySelector(':scope > .evidence');
    if (!panel) return;
    var title = panel.querySelector('h2');
    if (!panel.id) panel.id = 'evidence-panel';

    var sep = document.createElement('div');
    sep.className = 'splitter';
    sep.setAttribute('role', 'separator');
    sep.setAttribute('tabindex', '0');
    sep.setAttribute('aria-orientation', 'vertical');
    sep.setAttribute('aria-controls', panel.id);
    sep.setAttribute('aria-label', 'Resize the evidence panel' + (title ? ' (' + title.textContent + ')' : ''));
    sep.title = 'Drag to resize · Enter or . to hide · double-click to reset';
    sep.innerHTML = '<span class="grip" aria-hidden="true"></span>';
    panel.insertBefore(sep, panel.firstChild); // inside the panel's landmark; positioned against the shell

    var width = parseInt(load('iql-mock-ev-w'), 10) || DEFAULT;
    var hidden = load('iql-mock-ev-hidden') === '1';

    function max() { return Math.max(MIN, Math.min(720, Math.round(shell.clientWidth * 0.5))); }
    function apply(announce) {
      width = Math.max(MIN, Math.min(max(), width));
      shell.style.setProperty('--ev-w', width + 'px');
      shell.classList.toggle('ev-hidden', hidden);
      sep.setAttribute('aria-valuemin', '0');
      sep.setAttribute('aria-valuemax', String(max()));
      sep.setAttribute('aria-valuenow', hidden ? '0' : String(width));
      sep.setAttribute('aria-valuetext', hidden ? 'Evidence panel hidden' : 'Evidence panel ' + width + ' pixels wide');
      if (announce) { save('iql-mock-ev-w', String(width)); save('iql-mock-ev-hidden', hidden ? '1' : '0'); }
    }
    function toggle() { hidden = !hidden; apply(true); }

    sep.addEventListener('keydown', function (e) {
      var step = e.shiftKey ? STEP * 4 : STEP;
      if (e.key === 'ArrowLeft') { hidden = false; width += step; }
      else if (e.key === 'ArrowRight') { if (!hidden) width -= step; }
      else if (e.key === 'Home') { hidden = false; width = MIN; }
      else if (e.key === 'End') { hidden = false; width = max(); }
      else if (e.key === 'Enter') { toggle(); e.preventDefault(); return; }
      else return;
      e.preventDefault();
      apply(true);
    });
    sep.addEventListener('dblclick', function () { hidden = false; width = DEFAULT; apply(true); });

    var startX = 0, startW = 0, moved = false;
    sep.addEventListener('pointerdown', function (e) {
      if (e.button !== 0) return;
      startX = e.clientX; startW = hidden ? 0 : width; moved = false;
      sep.setPointerCapture(e.pointerId);
      sep.classList.add('dragging'); document.body.classList.add('resizing');
    });
    sep.addEventListener('pointermove', function (e) {
      if (!sep.hasPointerCapture(e.pointerId)) return;
      var w = startW + (startX - e.clientX);
      if (Math.abs(startX - e.clientX) > 3) moved = true;
      if (!moved) return;
      // Dragging well past the narrowest width hides the panel, as in most editors.
      if (w < MIN - 96) { hidden = true; } else { hidden = false; width = w; }
      apply(false);
    });
    function end(e) {
      if (!sep.hasPointerCapture(e.pointerId)) return;
      sep.releasePointerCapture(e.pointerId);
      sep.classList.remove('dragging'); document.body.classList.remove('resizing');
      if (!moved && hidden) hidden = false; // a click on the hidden edge brings it back
      apply(true);
    }
    sep.addEventListener('pointerup', end);
    sep.addEventListener('pointercancel', end);

    // "." anywhere outside a text field shows or hides the panel.
    document.addEventListener('keydown', function (e) {
      if (e.key !== '.' || e.metaKey || e.ctrlKey || e.altKey) return;
      var t = e.target;
      if (t && (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName))) return;
      toggle();
    });
    window.addEventListener('resize', function () { apply(false); });
    apply(false);
  });
})();
