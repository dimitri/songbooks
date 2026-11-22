// The lesson view (lessonview.go, play.html #lesson-tabs, #lesson-stage):
// a lesson's page has three views, Video, Score and Mixer, over the
// song's own render modes (song.js, #mode-segmented, hidden here):
//
// - Video is the Score mode with the lesson's video above the score, big,
//   the course's card beside it (under it on a phone);
// - Score is the Score mode alone (the card in a line above it);
// - Mixer is the Mixer mode.
//
// The tabs follow the mode, whoever changes it (the player's Mixer
// button, its "Open the mixer", the phone's sheets): the Mixer opened from
// the player is this Mixer tab, and leaving it comes back to the view it
// left.
//
// The video loads when played: its thumbnail until then (no YouTube on
// the page before), then YouTube's player, which plays on, docked in a
// corner, when its place is scrolled past or another view is shown, until
// its close button takes it away.
(function () {
  const tabs = document.getElementById('lesson-tabs');
  const sheet = document.getElementById('sheet');
  if (!tabs || !sheet) return;
  const stage = document.getElementById('lesson-stage');
  const home = document.getElementById('lesson-tabs-home');
  const seg = document.getElementById('mode-segmented');

  // ---- Where the tabs are -------------------------------------------------
  // On a wide screen in the chart's switcher's place (beside the song's
  // actions); on a phone, where that switcher doesn't show, at the top of
  // the song.
  const wide = window.matchMedia('(min-width: 901px)');
  function place() {
    if (!seg || !home) return;
    if (wide.matches) { if (tabs.nextElementSibling !== seg) seg.before(tabs); }
    else if (home.nextElementSibling !== tabs) home.after(tabs);
  }
  place();
  wide.addEventListener?.('change', place);

  // ---- Which view -----------------------------------------------------------
  const modeButton = (v) => seg && seg.querySelector(`button[data-value="${v}"]`);
  const hasVideo = !!tabs.querySelector('[data-lesson-tab="video"]');
  // The view the Score mode shows: the video's or the score alone.
  let scoreTab = tabs.dataset.tab === 'video' && hasVideo ? 'video' : 'score';
  function paint() {
    const mode = sheet.dataset.renderMode;
    const tab = mode === 'score' ? scoreTab : mode === 'mixer' ? 'mixer' : '';
    tabs.dataset.tab = tab;
    if (stage) stage.dataset.tab = tab;
    tabs.querySelectorAll('[data-lesson-tab]').forEach((b) => {
      const on = b.dataset.lessonTab === tab;
      b.classList.toggle('active', on);
      b.setAttribute('aria-pressed', String(on));
    });
    dock();
  }
  function show(tab) {
    if (tab === 'mixer') { modeButton('mixer')?.click(); return; }
    scoreTab = tab === 'video' && hasVideo ? 'video' : 'score';
    if (sheet.dataset.renderMode !== 'score') modeButton('score')?.click();
    paint();
    // The view's top: the video, or the score.
    const top = tab === 'video' ? stage : document.getElementById('gp-score-page');
    if (top && top.getBoundingClientRect().top < 0) top.scrollIntoView({ block: 'start', behavior: 'smooth' });
  }
  tabs.addEventListener('click', (e) => {
    const b = e.target.closest('[data-lesson-tab]');
    if (b) show(b.dataset.lessonTab);
  });
  new MutationObserver(paint).observe(sheet, { attributes: true, attributeFilter: ['data-render-mode'] });
  // A lesson opens on its file (song.js); here, on its video when it has one.
  if (sheet.dataset.renderMode !== 'score' && sheet.dataset.renderMode !== 'mixer') modeButton('score')?.click();

  // ---- The video --------------------------------------------------------------
  const fig = document.getElementById('lesson-stage-video');
  let inView = true;
  function dock() {
    if (!fig) return;
    const loaded = !!fig.querySelector('iframe');
    const docked = loaded && (tabs.dataset.tab !== 'video' || !inView);
    fig.classList.toggle('docked', docked);
    // Clear of the player docked at the bottom of the screen.
    const d = document.getElementById('audio-dock');
    const r = docked && d ? d.getBoundingClientRect() : null;
    fig.style.setProperty('--dock-clear', `${r && r.height ? Math.max(0, window.innerHeight - r.top) : 0}px`);
  }
  if (fig) {
    const frame = fig.querySelector('.lesson-stage-frame');
    const facade = fig.querySelector('.lesson-stage-play');
    facade.addEventListener('click', () => {
      const f = document.createElement('iframe');
      f.src = `https://www.youtube-nocookie.com/embed/${encodeURIComponent(fig.dataset.video)}?rel=0&autoplay=1&playsinline=1`;
      f.title = facade.getAttribute('aria-label') || t('The lesson’s video');
      f.allow = 'accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture';
      f.allowFullscreen = true;
      facade.hidden = true;
      frame.append(f);
      fig.classList.add('is-playing');
      dock();
    });
    fig.querySelector('.lesson-stage-close').addEventListener('click', () => {
      fig.querySelector('iframe')?.remove();
      facade.hidden = false;
      fig.classList.remove('is-playing');
      dock();
    });
    // Scrolled past (above the screen's top): docked.
    new IntersectionObserver(([e]) => {
      inView = e.isIntersecting || e.boundingClientRect.top >= 0;
      dock();
    }).observe(fig);
  }
  paint();
})();
