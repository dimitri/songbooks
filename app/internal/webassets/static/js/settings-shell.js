// The settings shell (settings_shell.html): a space's ⚙ page shows one
// section at a time, by the URL's #hash — on a wide screen the page's
// default section (data-default-section, else the first) when there's
// none, on a phone the list of sections. Without this script every
// section shows, one under the other.
//
//   SettingsShell.pick(hash, ids, phone, fallback)  the section to show
//                                                   ('' : the list)
//   SettingsShell.wire(layout, media)               shows it, and again
//                                                   on each #hash change
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else root.SettingsShell = api;
})(typeof self !== 'undefined' ? self : this, function () {
  // hash is location.hash ("#roles", "" …); ids the sections' ids, in
  // order; phone whether the list stands on its own.
  function pick(hash, ids, phone, fallback) {
    let id = '';
    try { id = decodeURIComponent(String(hash || '').replace(/^#/, '')); } catch (e) { id = ''; }
    if (ids.includes(id)) return id;
    if (phone) return '';
    return ids.includes(fallback) ? fallback : (ids[0] || '');
  }

  function wire(layout, media, win) {
    const w = win || window;
    const sections = [...layout.querySelectorAll('.admin-section')];
    const links = [...layout.querySelectorAll('[data-section-link]')];
    const ids = sections.map((s) => s.id);
    function show() {
      const id = pick(w.location.hash, ids, media.matches, layout.dataset.defaultSection);
      layout.classList.add('is-sectioned');
      layout.classList.toggle('has-section', !!id);
      sections.forEach((s) => { s.hidden = s.id !== id; });
      links.forEach((a) => {
        const on = a.dataset.sectionLink === id;
        a.classList.toggle('active', on);
        if (on) a.setAttribute('aria-current', 'page'); else a.removeAttribute('aria-current');
      });
      return id;
    }
    w.addEventListener('hashchange', () => { show(); w.scrollTo(0, 0); });
    if (media.addEventListener) media.addEventListener('change', show);
    show();
    return show;
  }

  if (typeof document !== 'undefined' && typeof module === 'undefined') {
    const layout = document.querySelector('[data-admin-layout]');
    if (layout) wire(layout, matchMedia('(max-width: 720px)'));
  }
  return { pick, wire };
});
