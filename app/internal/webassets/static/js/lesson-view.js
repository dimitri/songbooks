// The lesson view (lessonview.go, play.html #lesson-tabs, #lesson-stage).
//
// A wide screen: the lesson's video, the course's card under it, beside
// the score (about 40 / 60; above it on a narrower screen); the views,
// Score and Mixer, are the right-hand pane's (the Mixer full width), over
// the song's own render modes (song.js, #mode-segmented, hidden here).
//
// A phone (the app's phone width, 900 px and under, portrait tablets
// included): two screens of the same page, as the lesson apps do —
// "Leçon" (the video, the card, Practise) and "Pratique" (the score and
// its tools, ← Leçon); ?view=practice in the address for the second, so
// back, forward and a link shared work. Changing screens pauses what
// played.
//
// One player, the dock, plays the video ("Watch the lesson": its controls
// drive the YouTube player, through its IFrame API, a plain time ruler for
// the video's timeline) or the recording ("Play along": the dock as on
// every song, Recording / MIDI, the score's cursor following). One at a
// time (lesson-switch.js): choosing one pauses the other, and either
// starting (YouTube's own controls, the dock's play, a key) chooses it. On
// a phone the screen chooses: the lesson's watches, the practice one
// plays along.
//
// The video loads when played (its thumbnail until then); playing, it
// docks in a corner on the Mixer or once scrolled past, until its close
// button closes it.
(function () {
  const tabs = document.getElementById('lesson-tabs');
  const sheet = document.getElementById('sheet');
  if (!tabs || !sheet) return;
  const root = document.documentElement;
  const stage = document.getElementById('lesson-stage');
  const home = document.getElementById('lesson-tabs-home');
  const seg = document.getElementById('mode-segmented');
  const dock = document.getElementById('audio-dock');
  const phone = window.matchMedia('(max-width: 900px)');

  // ---- Where the tabs are -------------------------------------------------
  // On a wide screen in the chart's switcher's place (beside the song's
  // actions); on a phone, where that switcher doesn't show, at the top of
  // the song.
  function place() {
    if (!seg || !home) return;
    if (!phone.matches) { if (tabs.nextElementSibling !== seg) seg.before(tabs); }
    else if (home.nextElementSibling !== tabs) home.after(tabs);
  }
  place();

  // ---- The views: the score, the mixer ------------------------------------
  const modeButton = (v) => seg && seg.querySelector(`button[data-value="${v}"]`);
  // The chart's own views aren't the lesson's: whatever asks for one (the
  // player's Mixer button going back to "the chart" it left, a key) gets
  // the score.
  const CHART = ['normal', 'jazz', 'map', 'lyrics', 'drummer'];
  function paint() {
    const mode = sheet.dataset.renderMode;
    if (CHART.includes(mode) && modeButton('score')) { modeButton('score').click(); return; }
    const tab = mode === 'score' || mode === 'mixer' ? mode : '';
    tabs.dataset.tab = tab;
    if (stage) stage.dataset.tab = tab;
    tabs.querySelectorAll('[data-lesson-tab]').forEach((b) => {
      const on = b.dataset.lessonTab === tab;
      b.classList.toggle('active', on);
      b.setAttribute('aria-pressed', String(on));
    });
    dockVideo();
  }
  tabs.addEventListener('click', (e) => {
    const b = e.target.closest('[data-lesson-tab]');
    if (b && sheet.dataset.renderMode !== b.dataset.lessonTab) modeButton(b.dataset.lessonTab)?.click();
  });
  new MutationObserver(paint).observe(sheet, { attributes: true, attributeFilter: ['data-render-mode'] });
  if (sheet.dataset.renderMode !== 'score' && sheet.dataset.renderMode !== 'mixer') modeButton('score')?.click();

  // ---- The one player: the video or the recording ---------------------------
  const fig = document.getElementById('lesson-stage-video');
  const modeBox = document.getElementById('lesson-mode');
  const watch = document.getElementById('lesson-watch');
  const pauseDock = () => document.dispatchEvent(new CustomEvent('songbooks:pause-reference'));
  const sw = window.LessonSwitch.create({
    mode: fig && (!phone.matches || root.dataset.lessonScreen !== 'practice') ? 'watch' : 'play',
    pauseDock,
    pauseVideo: () => yt.pause(),
    onMode: () => showMode(),
  });
  document.addEventListener('songbooks:reference-playing', () => sw.dockPlaying());

  function showMode() {
    const watching = !!fig && sw.mode === 'watch';
    dock?.classList.toggle('is-watch', watching);
    if (watch) watch.hidden = !watching;
    modeBox?.querySelectorAll('[data-mode]').forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.mode === sw.mode)));
    // The switch sits at the start of what the dock shows.
    if (modeBox && dock) {
      const at = watching ? watch : dock.querySelector('.audio-transport');
      if (at && modeBox.parentElement !== at) at.prepend(modeBox);
      modeBox.hidden = false;
    }
  }
  if (fig && dock && watch) {
    dock.prepend(watch);
    modeBox?.addEventListener('click', (e) => {
      const b = e.target.closest('[data-mode]');
      if (b) sw.set(b.dataset.mode);
    });
  }
  showMode();

  // ---- The video: YouTube's player, through its IFrame API --------------------
  // Made when first played (its thumbnail until then); null without a video.
  const yt = (function () {
    if (!fig) return { pause() {}, play() {}, ready: () => false };
    const frame = fig.querySelector('.lesson-stage-frame');
    const facade = fig.querySelector('.lesson-stage-play');
    let player = null, made = null, timer = null, loop = { a: null, b: null };
    function api() {
      if (window.YT && window.YT.Player) return Promise.resolve();
      return new Promise((resolve) => {
        const prev = window.onYouTubeIframeAPIReady;
        window.onYouTubeIframeAPIReady = () => { prev?.(); resolve(); };
        if (!document.querySelector('script[data-youtube-api]')) {
          const s = document.createElement('script');
          s.src = 'https://www.youtube.com/iframe_api';
          s.dataset.youtubeApi = '1';
          document.head.append(s);
        }
      });
    }
    function make() {
      if (made) return made;
      made = api().then(() => new Promise((resolve) => {
        const host = document.createElement('div');
        host.className = 'lesson-yt';
        frame.append(host);
        facade.hidden = true;
        fig.classList.add('is-playing');
        player = new YT.Player(host, {
          host: 'https://www.youtube-nocookie.com',
          videoId: fig.dataset.video,
          playerVars: { rel: 0, playsinline: 1, modestbranding: 1 },
          events: {
            onReady: () => resolve(player),
            onStateChange: (e) => {
              // Played from its own controls too: the player is the video's.
              if (e.data === YT.PlayerState.PLAYING) sw.videoPlaying();
              render();
            },
          },
        });
        const f = frame.querySelector('iframe');
        if (f) f.title = facade.getAttribute('aria-label') || t('The lesson’s video');
        timer = setInterval(tick, 250);
        dockVideo();
      }));
      return made;
    }
    const playing = () => !!(player && player.getPlayerState && player.getPlayerState() === YT.PlayerState.PLAYING);
    function tick() {
      if (!player || !player.getCurrentTime) return;
      const now = player.getCurrentTime();
      if (playing() && loop.a !== null && loop.b !== null && now >= loop.b) player.seekTo(loop.a, true);
      render();
    }
    const fmt = (s) => { const x = Math.max(0, Math.floor(s || 0)); return `${Math.floor(x / 60)}:${String(x % 60).padStart(2, '0')}`; };
    function render() {
      if (!watch) return;
      const d = player && player.getDuration ? player.getDuration() : 0;
      const now = player && player.getCurrentTime ? player.getCurrentTime() : 0;
      const pct = (x) => `${d ? Math.max(0, Math.min(100, (x / d) * 100)) : 0}%`;
      watch.querySelector('.lw-time').textContent = `${fmt(now)} / ${fmt(d)}`;
      watch.querySelector('.lw-played').style.width = pct(now);
      watch.querySelector('.lw-head').style.left = pct(now);
      const shade = watch.querySelector('.lw-loop');
      shade.hidden = loop.a === null;
      if (loop.a !== null) {
        shade.style.left = pct(loop.a);
        shade.style.width = loop.b !== null ? `calc(${pct(loop.b)} - ${pct(loop.a)})` : '2px';
      }
      const btn = watch.querySelector('.lw-loop-btn');
      btn.setAttribute('aria-pressed', String(loop.a !== null));
      btn.classList.toggle('active', loop.a !== null);
      btn.querySelector('span').textContent = loop.a === null ? t('Cycle') : loop.b === null ? `${fmt(loop.a)}–…` : `${fmt(loop.a)}–${fmt(loop.b)}`;
      const pb = watch.querySelector('.lw-play');
      const on = playing();
      if (pb.dataset.on !== String(on)) {
        pb.dataset.on = String(on);
        pb.innerHTML = on ? '<svg class="icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><rect x="5" y="4" width="3.5" height="12" rx="1"/><rect x="11.5" y="4" width="3.5" height="12" rx="1"/></svg>' : pb.dataset.icon;
        pb.classList.toggle('active', on);
      }
    }
    if (watch) watch.querySelector('.lw-play').dataset.icon = watch.querySelector('.lw-play').innerHTML;
    const go = (fn) => make().then((p) => { fn(p); render(); });
    const self = {
      play() { sw.set('watch'); go((p) => p.playVideo()); },
      pause() { if (player && player.pauseVideo) player.pauseVideo(); },
      toggle() { if (playing()) self.pause(); else self.play(); },
      seek(s) { go((p) => p.seekTo(Math.max(0, s), true)); },
      by(ds) { if (player) self.seek(player.getCurrentTime() + ds); },
      start() { self.seek(loop.a !== null ? loop.a : 0); },
      loopTap() {
        const now = player ? player.getCurrentTime() : 0;
        if (loop.a === null) loop.a = now;
        else if (loop.b === null && now > loop.a + 0.5) loop.b = now;
        else loop = { a: null, b: null };
        render();
      },
      speed(r) { go((p) => p.setPlaybackRate(r)); },
      close() {
        clearInterval(timer);
        if (player && player.destroy) player.destroy();
        frame.querySelector('.lesson-yt')?.remove();
        player = null;
        made = null;
        facade.hidden = false;
        fig.classList.remove('is-playing');
        render();
        dockVideo();
      },
      ready: () => !!player,
      playing,
    };
    facade.addEventListener('click', () => self.play());
    fig.querySelector('.lesson-stage-close').addEventListener('click', () => self.close());
    if (watch) {
      watch.querySelector('.lw-play').addEventListener('click', () => self.toggle());
      watch.querySelector('.lw-start').addEventListener('click', () => self.start());
      watch.querySelector('.lw-back').addEventListener('click', () => self.by(-5));
      watch.querySelector('.lw-fwd').addEventListener('click', () => self.by(5));
      watch.querySelector('.lw-loop-btn').addEventListener('click', () => self.loopTap());
      watch.querySelector('.lw-speed select').addEventListener('change', (e) => self.speed(Number(e.target.value)));
      const ruler = watch.querySelector('.lw-ruler');
      ruler.addEventListener('click', (e) => {
        const r = ruler.getBoundingClientRect();
        const d = player && player.getDuration ? player.getDuration() : 0;
        if (d && r.width) self.seek(((e.clientX - r.left) / r.width) * d);
      });
      render();
    }
    // Watching, the player's keys are the video's.
    document.addEventListener('keydown', (e) => {
      if (sw.mode !== 'watch' || e.metaKey || e.ctrlKey || e.altKey || e.target.closest('input, textarea, select, [contenteditable]')) return;
      const k = e.key;
      const act = k === ' ' || k === 'k' || k === 'K' ? () => self.toggle()
        : k === 'Home' || k === '0' ? () => self.start()
          : k === 'ArrowLeft' || k === ',' ? () => self.by(-5)
            : k === 'ArrowRight' || k === '.' ? () => self.by(5) : null;
      if (!act) return;
      e.preventDefault();
      e.stopImmediatePropagation();
      act();
    }, true);
    return self;
  })();

  // Playing, out of sight (the Mixer, scrolled past): in a corner.
  let inView = true;
  function dockVideo() {
    if (!fig) return;
    const out = tabs.dataset.tab === 'mixer' || !inView;
    const docked = fig.classList.contains('is-playing') && out && !(phone.matches && root.dataset.lessonScreen === 'practice');
    fig.classList.toggle('docked', docked);
    // Clear of the player docked at the bottom of the screen.
    const r = docked && dock ? dock.getBoundingClientRect() : null;
    fig.style.setProperty('--dock-clear', `${r && r.height ? Math.max(0, window.innerHeight - r.top) : 0}px`);
  }
  if (fig) {
    new IntersectionObserver(([e]) => {
      inView = e.isIntersecting || e.boundingClientRect.top >= 0;
      dockVideo();
    }).observe(fig);
  }

  // ---- A phone's two screens ----------------------------------------------------
  function setScreen(screen, push) {
    const s = screen === 'practice' ? 'practice' : 'lesson';
    if (root.dataset.lessonScreen === s && !push) return;
    root.dataset.lessonScreen = s;
    if (push) {
      const url = new URL(location.href);
      if (s === tabs.dataset.screen) url.searchParams.delete('view'); else url.searchParams.set('view', s);
      history.pushState({ lessonScreen: s }, '', url);
    }
    if (phone.matches) {
      // What played on the other screen stops; this one's player is chosen.
      yt.pause();
      pauseDock();
      sw.set(s === 'practice' || !fig ? 'play' : 'watch');
      window.scrollTo(0, 0);
      // The score, drawn now that it has a width.
      if (s === 'practice') window.GPView?.showFull();
    }
    dockVideo();
  }
  document.addEventListener('click', (e) => {
    const b = e.target.closest('[data-screen]');
    if (b && (b.closest('#lesson-stage') || b.closest('#lesson-practice-head'))) setScreen(b.dataset.screen, true);
  });
  window.addEventListener('popstate', () => {
    const v = new URLSearchParams(location.search).get('view');
    setScreen(v === 'practice' || v === 'lesson' ? v : tabs.dataset.screen, false);
  });
  phone.addEventListener?.('change', () => {
    place();
    // A phone's screen chooses the player; a wide screen keeps the choice.
    if (phone.matches) sw.set(root.dataset.lessonScreen === 'practice' || !fig ? 'play' : 'watch');
    dockVideo();
  });

  paint();
  window.LessonView = { switcher: sw, video: yt, setScreen };
})();
