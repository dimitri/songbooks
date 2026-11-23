// band-votes.js — a band's votes on its proposals (docs/band-votes.md;
// internal/store/band_proposals.go has the rules).
//
// Each member rates a proposed song 1 to 4 stars — an even scale, never
// right in the middle: 1 and 2 are against it, 3 and 4 for it — or says
// "No thanks", a refusal shown apart from the stars, or hasn't voted
// (never 0 stars). The same vote control and the same summary (the
// average, for/against, how many voted of the band's members, who said
// no thanks, who hasn't voted) show on the To vote list, a proposal's
// page, the vote's player (playlist.html, keys 1–4 and N) and the
// listening party, where each member's phone follows what the host plays.
//
// BandVotes.control(el, opts) — the vote control in el.
// BandVotes.summary(el, tally, opts) — where the vote stands, in el.
// BandVotes.list() — the To vote list's cards (data-proposal, data-tally).
// BandVotes.player(opts) — the vote's player (playlist.html, .Vote).
// BandVotes.party(opts) — the listening party's page.
//
// The plain logic (keys, meanings, the meter) is tested by
// internal/webassets/jstest/band-votes.test.mjs.
(function (root) {
  const MAX = 4;
  const tr = (s, ...a) => (root.t ? root.t(s, ...a) : s.replace(/%[ds]/g, () => String(a.shift())));
  const trn = (n, one, other) => (root.tn ? root.tn(n, one, other) : tr(n === 1 ? one : other, n));

  // ---- plain logic ----------------------------------------------------

  // The vote a key gives: 1–4 stars, N no thanks; null for any other.
  function voteForKey(key) {
    if (/^[1-4]$/.test(key)) return { stars: Number(key) };
    if (key === 'n' || key === 'N') return { noThanks: true };
    return null;
  }
  // What n stars say.
  const MEANINGS = ['', 'Not for us', 'Rather not', 'Yes, let’s', 'Love it'];
  const meaning = (n) => MEANINGS[n] || '';
  // How full the average's 4 stars are, in percent.
  const fill = (avg) => Math.max(0, Math.min(100, Math.round((avg / MAX) * 1000) / 10));
  // "3/4": how many of the members voted.
  const votedText = (tally) => `${tally.voted || 0}/${tally.members || 0}`;
  // A vote as the server takes it, from the control's state.
  const body = (v) => (v.clear ? { clear: true } : v.noThanks ? { noThanks: true } : { stars: v.stars });
  // A key pressed in a field types, it doesn't vote.
  const typing = (target) => !!target && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName || ''));

  const logic = { voteForKey, meaning, fill, votedText, body, typing, MAX };
  if (typeof module !== 'undefined' && module.exports) { module.exports = logic; return; }

  // ---- the page --------------------------------------------------------

  const el = (tag, cls, text) => {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  };
  const STAR = '★';

  async function send(bandId, proposalId, v) {
    const res = await fetch(`/api/bands/${bandId}/proposals/${proposalId}/votes`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body(v)),
    }).catch(() => null);
    const data = res ? await res.json().catch(() => ({})) : {};
    if (!res || !res.ok) throw new Error(data.message || tr('Couldn’t record your vote. Check your connection and try again.'));
    return data;
  }
  async function tallyOf(bandId, proposalId) {
    const res = await fetch(`/api/bands/${bandId}/proposals/${proposalId}/tally`).catch(() => null);
    return res && res.ok ? res.json() : null;
  }

  // The average as 4 stars, partly filled, and its number.
  function meter(avg, avgText) {
    const m = el('span', 'stars-meter');
    m.setAttribute('role', 'img');
    m.setAttribute('aria-label', avgText ? tr('Average %s stars out of 4', avgText) : tr('No stars yet'));
    const back = el('span', 'stars-meter-back', STAR.repeat(MAX));
    const front = el('span', 'stars-meter-front', STAR.repeat(MAX));
    front.style.width = `${fill(avg || 0)}%`;
    back.setAttribute('aria-hidden', 'true');
    front.setAttribute('aria-hidden', 'true');
    m.append(back, front);
    return m;
  }

  // Where the vote stands: the average, for/against, voted of members,
  // the no thanks apart, who hasn't voted. opts.status: a decided one's.
  function summary(box, tally, opts = {}) {
    box.replaceChildren();
    if (!tally) return;
    const head = el('div', 'vote-sum-head');
    head.append(meter(tally.avg, tally.avgText));
    if (tally.ratings) {
      head.append(el('strong', 'vote-avg', tally.avgText));
      const split = el('span', 'vote-split');
      split.append(el('span', tally.positive ? 'is-for' : '', tr('%d for', tally.positive)), ' · ', el('span', tally.negative ? 'is-against' : '', tr('%d against', tally.negative)));
      head.append(split);
    } else {
      head.append(el('span', 'vote-none', tr('No stars yet')));
    }
    const voted = el('span', 'vote-count');
    voted.append(el('strong', '', votedText(tally)), ' ', el('span', 'vote-count-word', tr('voted')));
    voted.title = tr('%d of the band’s %d core members voted', tally.voted || 0, tally.members || 0);
    voted.setAttribute('aria-label', voted.title);
    head.append(voted);
    box.append(head);

    if (opts.full && tally.ratings) {
      // How many gave each number of stars, 4 at the top.
      const hist = el('div', 'vote-hist');
      const top = Math.max(1, ...tally.hist);
      for (let n = MAX; n >= 1; n--) {
        const c = tally.hist[n - 1] || 0;
        const row = el('div', 'vote-hist-row' + (n * 2 > MAX ? ' is-for' : ' is-against'));
        row.setAttribute('aria-label', trn(n, '%d star', '%d stars') + ': ' + c);
        const bar = el('span', 'vote-hist-bar');
        bar.append(el('span', 'vote-hist-fill'));
        bar.firstChild.style.width = `${(c / top) * 100}%`;
        row.append(el('span', 'vote-hist-label', STAR.repeat(n)), bar, el('span', 'vote-hist-n', String(c)));
        hist.append(row);
      }
      box.append(hist);
    }
    const lines = el('div', 'vote-sum-lines');
    if (tally.noThanks && tally.noThanks.length) {
      const p = el('p', 'vote-line vote-line-no');
      p.append(el('span', 'vote-chip is-no', trn(tally.noThanks.length, '%d no thanks', '%d no thanks')),
        ' ', el('span', 'vote-names', tally.noThanks.map((b) => b.name).join(', ')));
      lines.append(p);
    }
    if (opts.full && tally.rated && tally.rated.length) {
      const p = el('p', 'vote-line');
      p.append(el('span', 'vote-chip', tr('Stars')), ' ',
        el('span', 'vote-names', tally.rated.map((b) => `${b.name} ${STAR.repeat(b.stars)}`).join(', ')));
      lines.append(p);
    }
    if (opts.status === 'open' || !opts.status) {
      if (tally.notVoted && tally.notVoted.length) {
        const p = el('p', 'vote-line vote-line-wait');
        p.append(el('span', 'vote-chip is-wait', tr('Not voted')), ' ', el('span', 'vote-names', tally.notVoted.join(', ')));
        lines.append(p);
      }
      if (tally.blocked) {
        lines.append(el('p', 'vote-line vote-blocked', tr('Held back: no thanks from %s', tally.blockedBy.join(', '))));
      }
    }
    if (lines.children.length) box.append(lines);
  }

  // The vote control: 4 stars, No thanks, Take back my vote. opts:
  // {bandId, proposalId, title, mine: {stars, noThanks} | null,
  //  onVoted(answer)}. Returns {set(vote), vote(v)} for keys.
  function control(box, opts) {
    box.replaceChildren();
    box.classList.add('vote-ctl');
    box.setAttribute('role', 'group');
    box.setAttribute('aria-label', opts.title ? tr('Your vote on “%s”', opts.title) : tr('Your vote'));
    let mine = opts.mine ? { stars: opts.mine.stars || 0, noThanks: !!opts.mine.noThanks } : null;
    const row = el('div', 'vote-ctl-row');
    const stars = el('div', 'vote-stars');
    const buttons = [];
    for (let n = 1; n <= MAX; n++) {
      const b = el('button', 'vote-star', STAR);
      b.type = 'button';
      b.dataset.stars = n;
      b.setAttribute('aria-label', trn(n, '%d star', '%d stars') + ' — ' + tr(meaning(n)));
      b.title = tr(meaning(n));
      b.addEventListener('click', () => cast({ stars: n }));
      b.addEventListener('pointerenter', () => preview(n));
      b.addEventListener('focus', () => preview(n));
      b.addEventListener('blur', () => preview(0));
      buttons.push(b);
      stars.append(b);
    }
    stars.addEventListener('pointerleave', () => preview(0));
    const no = el('button', 'vote-nothanks', tr('No thanks'));
    no.type = 'button';
    no.addEventListener('click', () => cast(mine && mine.noThanks ? { clear: true } : { noThanks: true }));
    row.append(stars, no);
    const foot = el('div', 'vote-ctl-foot');
    const said = el('span', 'vote-said');
    const clear = el('button', 'link-button vote-clear', tr('Take back my vote'));
    clear.type = 'button';
    clear.addEventListener('click', () => cast({ clear: true }));
    foot.append(said, clear);
    const status = el('p', 'form-status vote-status');
    status.setAttribute('role', 'status');
    box.append(row, foot, status);

    function preview(n) {
      const shown = n || (mine && mine.stars) || 0;
      buttons.forEach((b, i) => b.classList.toggle('is-on', i < shown));
      stars.classList.toggle('is-preview', !!n);
      said.textContent = n ? tr(meaning(n)) : mine ? (mine.noThanks ? tr('You said no thanks') : tr('Your vote: %s', tr(meaning(mine.stars)))) : tr('Not voted yet');
    }
    function draw() {
      buttons.forEach((b, i) => {
        b.setAttribute('aria-pressed', String(!!mine && mine.stars === i + 1));
      });
      no.setAttribute('aria-pressed', String(!!mine && mine.noThanks));
      no.classList.toggle('is-on', !!mine && mine.noThanks);
      clear.hidden = !mine;
      box.classList.toggle('has-vote', !!mine);
      preview(0);
    }
    async function cast(v) {
      const before = mine;
      mine = v.clear ? null : { stars: v.stars || 0, noThanks: !!v.noThanks };
      draw();
      Forms.status(status, '');
      box.setAttribute('aria-busy', 'true');
      try {
        const answer = await send(opts.bandId, opts.proposalId, v);
        mine = answer.tally && answer.tally.mine ? { stars: answer.tally.mine.stars || 0, noThanks: !!answer.tally.mine.noThanks } : null;
        draw();
        if (answer.status === 'accepted') Forms.status(status, tr('That was the vote it needed: it’s in the songbook.'), 'ok');
        else if (answer.status === 'rejected') Forms.status(status, tr('The votes turned it down.'), 'ok');
        else Forms.status(status, v.clear ? tr('Vote taken back.') : tr('Saved.'), 'ok');
        if (opts.onVoted) opts.onVoted(answer);
      } catch (e) {
        mine = before;
        draw();
        Forms.status(status, e.message, 'error');
      } finally {
        box.removeAttribute('aria-busy');
      }
    }
    draw();
    return { set(m) { mine = m ? { stars: m.stars || 0, noThanks: !!m.noThanks } : null; draw(); }, vote: cast, el: box };
  }

  // Keys 1–4 and N vote on what getCtl() returns (outside a field).
  function keys(getCtl) {
    document.addEventListener('keydown', (e) => {
      if (e.metaKey || e.ctrlKey || e.altKey || typing(e.target)) return;
      const v = voteForKey(e.key);
      const ctl = v && getCtl();
      if (!ctl) return;
      e.preventDefault();
      ctl.vote(v);
    });
  }

  // ---- the To vote list ----------------------------------------------

  function list() {
    for (const card of document.querySelectorAll('.vote-card[data-proposal]')) {
      const bandId = Number(card.dataset.band), proposalId = Number(card.dataset.proposal);
      let tally = null;
      try { tally = JSON.parse(card.dataset.tally || 'null'); } catch (e) {}
      const sum = card.querySelector('[data-sum]');
      const status = card.dataset.status;
      const full = sum.classList.contains('is-full');
      summary(sum, tally, { status, full });
      const ctlBox = card.querySelector('[data-ctl]'); // a core member's only
      if (ctlBox && status === 'open') {
        control(ctlBox, {
          bandId, proposalId, title: card.dataset.title, mine: tally && tally.mine,
          onVoted: (a) => {
            summary(sum, a.tally, { status: a.status, full });
            if (a.status !== 'open') {
              card.dataset.status = a.status;
              ctlBox.replaceChildren(el('p', 'vote-decided is-' + a.status, a.status === 'accepted' ? tr('Accepted: it’s in the songbook.') : tr('Turned down by the votes.')));
            }
          },
        });
      }
    }
  }

  // ---- the vote's player (playlist.html, .Vote) ----------------------

  function player(opts) {
    const bar = document.getElementById('pl-vote');
    const panel = document.getElementById('vote-panel-sum');
    const panelNote = document.getElementById('vote-panel-note');
    const panelLinks = document.getElementById('vote-panel-links');
    const tallies = opts.tallies || {};
    let ctl = null, current = null;
    function scores() {
      document.querySelectorAll('[data-score]').forEach((s) => {
        const ty = tallies[s.dataset.score];
        s.replaceChildren();
        if (!ty) return;
        const mine = ty.mine ? (ty.mine.noThanks ? tr('no thanks') : STAR.repeat(ty.mine.stars)) : '';
        s.append(el('span', 'pl-score-avg', ty.ratings ? `${STAR} ${ty.avgText}` : `${STAR} –`), el('span', 'pl-score-count', votedText(ty)));
        if (mine) s.append(el('span', 'pl-score-mine', mine));
        s.title = ty.mine ? tr('You voted') : tr('Not voted yet');
      });
    }
    function show(track) {
      if (!track || !track.proposal) return;
      current = track;
      const id = String(track.proposal);
      const ty = tallies[id];
      if (opts.canVote) {
        ctl = control(bar, {
          bandId: opts.bandId, proposalId: track.proposal, title: track.title, mine: ty && ty.mine,
          onVoted: (a) => { tallies[id] = a.tally; summary(panel, a.tally, { status: a.status, full: true }); scores(); },
        });
        bar.append(el('p', 'vote-keys hint', tr('Keys: 1 to 4 stars, N for no thanks')));
      } else {
        bar.replaceChildren(el('p', 'vote-guest-note', tr('You’re a guest member: you listen along; the core members vote.')));
      }
      if (panel) summary(panel, ty, { status: 'open', full: true });
      if (panelNote) { panelNote.textContent = track.note || ''; panelNote.hidden = !track.note; }
      if (panelLinks) {
        panelLinks.replaceChildren();
        const open = el('a', '', tr('Open the proposal'));
        open.href = track.href;
        panelLinks.append(open);
        if (track.watch) {
          const w = el('a', '', tr('Watch on YouTube'));
          w.href = track.watch; w.target = '_blank'; w.rel = 'noopener';
          panelLinks.append(w);
        }
      }
      if (opts.party) host();
    }
    // The listening party's host: what plays is on every member's phone.
    let lastSent = '';
    function host() {
      const pl = root.SongbooksPlaylist;
      if (!pl || !current) return;
      const state = `${current.proposal}:${pl.playing() ? 1 : 0}`;
      if (state === lastSent) return;
      lastSent = state;
      fetch(`/api/bands/${opts.bandId}/party`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ proposalId: current.proposal, eventId: opts.eventId || 0, playing: pl.playing() }),
      }).catch(() => {});
    }
    document.addEventListener('playlist:track', (e) => show(e.detail.track));
    if (opts.party) setInterval(() => { lastSent = lastSent.replace(/:.*/, ':?'); host(); }, 15000);
    if (opts.party) setInterval(host, 1000);
    // Others' votes come in: the one playing's, every few seconds.
    setInterval(async () => {
      if (!current || document.hidden) return;
      const a = await tallyOf(opts.bandId, current.proposal);
      if (!a) return;
      tallies[String(current.proposal)] = a.tally;
      if (panel) summary(panel, a.tally, { status: a.status, full: true });
      scores();
    }, 6000);
    keys(() => ctl);
    scores();
    if (root.SongbooksPlaylist) show(root.SongbooksPlaylist.track());
  }

  // ---- the listening party ---------------------------------------------

  function party(opts) {
    const box = document.getElementById('party-now');
    const title = document.getElementById('party-now-title');
    const sub = document.getElementById('party-now-sub');
    const thumb = document.getElementById('party-now-thumb');
    const sum = document.getElementById('party-now-sum');
    const ctlBox = document.getElementById('party-now-ctl');
    const note = document.getElementById('party-now-note');
    const state = document.getElementById('party-now-state');
    const cards = {};
    document.querySelectorAll('.vote-card[data-proposal]').forEach((c) => { cards[c.dataset.proposal] = c; });
    let ctl = null, shown = 0, synced = false;
    function render(answer) {
      const now = answer && answer.now;
      const card = now && now.proposalId ? cards[String(now.proposalId)] : null;
      box.classList.toggle('is-idle', !card);
      if (!card) {
        shown = 0;
        ctl = null;
        title.textContent = tr('Nothing playing yet');
        sub.textContent = tr('When the host plays a song, it shows here, to rate while it plays.');
        state.textContent = '';
        thumb.hidden = true;
        sum.replaceChildren(); ctlBox.replaceChildren(); note.hidden = true;
        return;
      }
      state.textContent = now.playing ? tr('Playing now · %s hosts', now.host) : tr('Paused · %s hosts', now.host);
      state.classList.toggle('is-playing', !!now.playing);
      if (shown !== now.proposalId) {
        shown = now.proposalId;
        synced = !!answer.tally;
        title.textContent = card.dataset.title;
        sub.textContent = card.dataset.sub || '';
        const img = card.querySelector('.vote-thumb img');
        thumb.hidden = !img;
        if (img) thumb.src = img.src;
        note.textContent = card.dataset.note || '';
        note.hidden = !card.dataset.note;
        if (opts.canVote) {
          ctl = control(ctlBox, {
            bandId: opts.bandId, proposalId: now.proposalId, title: card.dataset.title, mine: answer.tally && answer.tally.mine,
            onVoted: (a) => { summary(sum, a.tally, { status: a.status, full: true }); refreshCard(card, a); },
          });
          ctlBox.append(el('p', 'vote-keys hint', tr('Keys: 1 to 4 stars, N for no thanks')));
        }
        Array.from(Object.values(cards)).forEach((c) => c.classList.toggle('is-now', c === card));
      }
      if (answer.tally) {
        if (!synced && ctl) { ctl.set(answer.tally.mine); synced = true; }
        summary(sum, answer.tally, { status: answer.status, full: true });
        refreshCard(card, answer);
      }
    }
    function refreshCard(card, a) {
      const s = card.querySelector('[data-sum]');
      if (s && a.tally) summary(s, a.tally, { status: a.status });
    }
    async function poll() {
      if (document.hidden) return;
      const res = await fetch(`/api/bands/${opts.bandId}/party`).catch(() => null);
      if (res && res.ok) render(await res.json().catch(() => null));
    }
    render(opts.now ? { now: opts.now } : null);
    poll();
    setInterval(poll, 3000);
    keys(() => ctl);
  }

  root.BandVotes = { control, summary, list, player, party, keys, ...logic };
})(typeof window !== 'undefined' ? window : globalThis);
