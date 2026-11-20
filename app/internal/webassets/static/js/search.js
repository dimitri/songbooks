// Search: one palette for the whole app (menu proposal §3.8, search.go).
// ⌘K or Ctrl-K anywhere, the sidebar's "Rechercher… ⌘K" field, or the 🔍
// in a phone's top bar open it (full screen on a phone). It finds songs,
// songbooks, set lists, groups, events and a coach's students — the
// current space's first — and commands, each with its shortcut. The
// shortcuts work without opening it: g then a letter goes somewhere, n
// then a letter makes something, ⇧S is stage mode on a song.
//
// Loaded in every page's <head> (pwa-head), so its keys are seen before
// the player's: "g m" mustn't also switch the click on. It does nothing
// on a page without the app's navigation (the sign-in pages).
(function () {
  'use strict';
  var tr = function (s) { return typeof window.t === 'function' ? window.t(s) : s; };
  var isMac = /Mac|iPhone|iPad/.test(navigator.platform || '');
  var inApp = function () { return !!document.getElementById('desktop-sidebar'); };

  // The space the page belongs to: its finds come first.
  function currentSpace() {
    var m = location.pathname.match(/^\/(band|communitie)s\/(\d+)/);
    if (!m) return '';
    return (m[1] === 'band' ? 'band' : 'community') + ':' + m[2];
  }
  function stageToggle() { return document.querySelector('.stage-mode-toggle'); }

  // ---- Shortcuts -----------------------------------------------------------
  var GO = { h: '/home', l: '/catalog', a: '/calendar', n: '/notifications', b: '/metronome', t: '/tuner' };
  var NEW = { l: '/new/lesson', s: '/new/setlist' };
  var pending = null, pendingAt = 0;
  function typing(el) {
    return el && el.closest && el.closest('input:not([type="range"]):not([type="checkbox"]):not([type="radio"]), select, textarea, [contenteditable=""], [contenteditable="true"]');
  }
  function withSpace(url) {
    var sp = currentSpace();
    return sp ? url + '?space=' + encodeURIComponent(sp) : url;
  }
  window.addEventListener('keydown', function (e) {
    if (!inApp()) return;
    var k = (e.key || '').toLowerCase();
    if ((e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey && k === 'k') {
      e.preventDefault();
      e.stopImmediatePropagation();
      if (dialog && dialog.open) close(); else open();
      return;
    }
    if (e.metaKey || e.ctrlKey || e.altKey || e.defaultPrevented || e.isComposing) return;
    if (typing(e.target) || (dialog && dialog.open)) return;
    // A dialog or the song's editor has the keys.
    if (document.querySelector('dialog[open], #edit-drawer:not([hidden])')) return;
    if (e.shiftKey && k === 's' && stageToggle()) {
      e.preventDefault();
      e.stopImmediatePropagation();
      stageToggle().click();
      return;
    }
    if (e.shiftKey) { pending = null; return; }
    var now = Date.now();
    if (pending && now - pendingAt < 1500) {
      var map = pending === 'g' ? GO : NEW;
      pending = null;
      if (map[k]) {
        e.preventDefault();
        e.stopImmediatePropagation();
        location.href = map === NEW ? withSpace(map[k]) : map[k];
      }
      return;
    }
    pending = (k === 'g' || k === 'n') ? k : null;
    pendingAt = now;
  }, true);

  // ---- The palette ---------------------------------------------------------
  var dialog = null, input = null, list = null, status = null;
  var items = [], active = -1, seq = 0, timer = null, opener = null, refocusing = false, closedAt = 0;

  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text != null) n.textContent = text;
    return n;
  }
  function keyLabel(k) {
    if (k === 'Shift+s') return '⇧S';
    return k.length === 1 ? k.toUpperCase() : k;
  }

  function build() {
    dialog = el('dialog', 'search-palette');
    dialog.setAttribute('aria-label', tr('nav|Search'));
    var box = el('div', 'search-palette-box');
    var head = el('div', 'search-palette-head');
    var icon = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    icon.setAttribute('class', 'icon');
    icon.setAttribute('viewBox', '0 0 20 20');
    icon.setAttribute('aria-hidden', 'true');
    icon.innerHTML = '<circle cx="8.6" cy="8.6" r="5.2" fill="none" stroke="currentColor" stroke-width="1.7"/><path d="m12.5 12.5 4.3 4.3" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/>';
    input = el('input', 'search-palette-input');
    input.type = 'search';
    input.autocomplete = 'off';
    input.spellcheck = false;
    input.placeholder = tr('Songs, set lists, groups, events…');
    input.setAttribute('role', 'combobox');
    input.setAttribute('aria-expanded', 'true');
    input.setAttribute('aria-controls', 'search-palette-list');
    input.setAttribute('aria-autocomplete', 'list');
    input.setAttribute('aria-label', tr('nav|Search'));
    var closeBtn = el('button', 'search-palette-close', tr('Close'));
    closeBtn.type = 'button';
    closeBtn.addEventListener('click', close);
    head.append(icon, input, closeBtn);
    list = el('div', 'search-palette-list');
    list.id = 'search-palette-list';
    list.setAttribute('role', 'listbox');
    list.setAttribute('aria-label', tr('Results'));
    status = el('p', 'search-palette-status');
    status.setAttribute('role', 'status');
    var foot = el('div', 'search-palette-foot');
    foot.append(hint('↑↓', tr('to move')), hint('↵', tr('to open')), hint('Esc', tr('to close')));
    box.append(head, list, status, foot);
    dialog.append(box);
    document.body.append(dialog);

    input.addEventListener('input', function () {
      clearTimeout(timer);
      timer = setTimeout(load, 140);
    });
    input.addEventListener('keydown', function (e) {
      if (e.key === 'ArrowDown') { e.preventDefault(); move(1); }
      else if (e.key === 'ArrowUp') { e.preventDefault(); move(-1); }
      else if (e.key === 'Enter') { e.preventDefault(); if (items[active]) go(items[active]); }
      else if (e.key === 'Escape') { e.preventDefault(); close(); } // a search field would only clear itself
    });
    dialog.addEventListener('close', function () {
      document.documentElement.classList.remove('search-open');
      // Back where it was opened from; the sidebar's field mustn't open
      // it again by getting the focus back.
      if (opener && opener.focus) { refocusing = true; opener.focus(); refocusing = false; }
    });
    // A click on the backdrop closes it.
    dialog.addEventListener('click', function (e) { if (e.target === dialog) close(); });
    dialog.addEventListener('cancel', function () { closedAt = Date.now(); });
  }
  function hint(k, text) {
    var s = el('span');
    s.append(el('kbd', null, k), document.createTextNode(' ' + text));
    return s;
  }

  function open(q) {
    if (!inApp()) return;
    if (!dialog) build();
    opener = document.activeElement;
    document.documentElement.classList.add('search-open');
    if (!dialog.open) dialog.showModal();
    input.value = q || '';
    input.focus();
    load();
  }
  function close() { closedAt = Date.now(); if (dialog && dialog.open) dialog.close(); }

  function load() {
    var q = input.value.trim();
    var my = ++seq;
    var url = '/api/search?q=' + encodeURIComponent(q);
    var sp = currentSpace();
    if (sp) url += '&space=' + encodeURIComponent(sp);
    if (q.length >= 2) status.textContent = tr('Searching…');
    fetch(url, { credentials: 'same-origin', headers: { Accept: 'application/json' } })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (data) { if (my === seq && data) render(data, q); })
      .catch(function () { if (my === seq) status.textContent = tr('Couldn’t reach the server. Check your connection and try again.'); });
  }

  function render(data, q) {
    list.textContent = '';
    items = [];
    var groups = (data.groups || []).slice();
    var cmds = (data.commands || []).filter(function (c) { return c.action !== 'stage' || stageToggle(); });
    // With nothing typed, the commands lead; with a query, the finds do.
    var cmdGroup = cmds.length ? { kind: 'command', label: data.commandsLabel || tr('Commands'), items: cmds } : null;
    if (cmdGroup) { if (q) groups.push(cmdGroup); else groups.unshift(cmdGroup); }
    groups.forEach(function (g) {
      var sec = el('div', 'search-palette-group');
      sec.setAttribute('role', 'group');
      var hid = 'search-palette-g-' + g.kind;
      var h = el('div', 'search-palette-group-label', g.label);
      h.id = hid;
      sec.setAttribute('aria-labelledby', hid);
      sec.append(h);
      g.items.forEach(function (it) {
        var i = items.length;
        var row = el(it.url ? 'a' : 'div', 'search-palette-row');
        row.id = 'search-palette-opt-' + i;
        row.setAttribute('role', 'option');
        row.setAttribute('aria-selected', 'false');
        if (it.url) { row.href = it.url; row.tabIndex = -1; }
        var main = el('span', 'search-palette-row-main');
        main.append(el('span', 'search-palette-row-title', it.title));
        if (it.subtitle) main.append(el('span', 'search-palette-row-sub', it.subtitle));
        row.append(main);
        if (it.here) row.append(el('span', 'search-here', tr('here')));
        if (it.keys && it.keys.length) {
          var keys = el('span', 'search-keys');
          it.keys.forEach(function (k) { keys.append(el('kbd', null, keyLabel(k))); });
          row.append(keys);
        }
        row.addEventListener('click', function (e) { e.preventDefault(); go(it); });
        row.addEventListener('mousemove', function () { if (active !== i) setActive(i); });
        sec.append(row);
        items.push(it);
      });
      list.append(sec);
    });
    if (!items.length) status.textContent = q.length < 2 ? tr('Type at least two letters.') : tr('Nothing found.');
    else status.textContent = q.length >= 2 && typeof window.tn === 'function' ? window.tn(items.length, '%d result', '%d results') : '';
    setActive(items.length ? 0 : -1);
  }

  function setActive(i) {
    var prev = list.querySelector('[aria-selected="true"]');
    if (prev) prev.setAttribute('aria-selected', 'false');
    active = i;
    if (i < 0) { input.removeAttribute('aria-activedescendant'); return; }
    var row = document.getElementById('search-palette-opt-' + i);
    row.setAttribute('aria-selected', 'true');
    input.setAttribute('aria-activedescendant', row.id);
    row.scrollIntoView({ block: 'nearest' });
  }
  function move(d) {
    if (!items.length) return;
    setActive((active + d + items.length) % items.length);
  }
  function go(it) {
    if (it.action === 'stage') { close(); if (stageToggle()) stageToggle().click(); return; }
    if (!it.url) return;
    var url = it.url;
    if (/^\/new\//.test(url)) url = withSpace(url);
    location.href = url;
  }

  // ---- Openers -------------------------------------------------------------
  document.addEventListener('click', function (e) {
    var t = e.target.closest && e.target.closest('[data-search-open]');
    if (!t || !inApp() || e.metaKey || e.ctrlKey || e.shiftKey) return;
    e.preventDefault();
    if (dialog && dialog.open) return; // the field's focus opened it already
    open(t.value || '');
  });
  document.addEventListener('focusin', function (e) {
    var t = e.target;
    // The browser gives the focus back to the field the palette was
    // opened from as it closes: that mustn't open it again.
    if (Date.now() - closedAt < 500) return;
    if (!refocusing && t && t.matches && t.matches('input[data-search-open]') && inApp()) open(t.value || '');
  });
  document.addEventListener('DOMContentLoaded', function () {
    // ⌘K on a Mac, Ctrl K elsewhere.
    document.querySelectorAll('[data-search-kbd]').forEach(function (k) { k.textContent = isMac ? '⌘K' : 'Ctrl K'; });
    // "New lesson" and "New set list" land on their page with its form
    // open (/new/… redirects to #new or #new-band-17).
    var m = location.hash.match(/^#new(?:-band-(\d+))?$/);
    if (!m) return;
    var form = m[1] ? document.querySelector('form[data-band-id="' + m[1] + '"]') : null;
    var det = form ? form.closest('details') : document.querySelector('details.coach-new-lesson, details.setlist-new');
    if (!det) return;
    det.open = true;
    det.scrollIntoView({ block: 'center' });
    var first = det.querySelector('input:not([type="hidden"]), select, textarea');
    if (first) first.focus();
  });
})();
