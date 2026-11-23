// band-votes.js — a band's votes on its proposals (docs/band-votes.md;
// internal/store/band_proposals.go has the rules).
//
// Each core member says "No thanks" — a veto, the strongest no: first,
// apart from the stars, a thumbs down (icon-no-thanks) — or rates a proposed
// song 1 to 4 stars — an even scale, never right in the middle: 1 and 2
// are against it, 3 and 4 for it —, or hasn't voted (never 0 stars). The
// votes are information: any member decides by hand (decide()). The same
// vote control and the same summary (the average, for/against, how many
// core members voted, who said no thanks, who hasn't voted — the server
// sends only the count until one has voted) show on the To vote list, a
// proposal's page and the vote's player (playlist.html, keys 1–4 and N).
//
// BandVotes.control(el, opts) — the vote control in el.
// BandVotes.summary(el, tally, opts) — where the vote stands, in el.
// BandVotes.list() — the To vote list's cards (data-proposal, data-tally).
// BandVotes.player(opts) — the vote's player (playlist.html, .Vote).
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
  // A no thanks: a thumbs down (icons.html's icon-no-thanks), drawn to
  // sit with the stars: the same size, the same states.
  const NO = '<svg class="icon icon-no-thanks" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><rect x="2" y="3" width="4" height="11" rx="1"/><path d="M8 4.5C8 3.67 8.67 3 9.5 3H17c.8 0 1.5.5 1.8 1.2l2.6 6.4c.1.3.2.5.2.8V13c0 1.1-.9 2-2 2h-5.2l.8 3.9c.2.9-.3 1.8-1.1 2.2-.5.2-1.1 0-1.4-.4L8.5 15.4c-.3-.4-.5-.9-.5-1.4z"/></svg>';
  const noIcon = () => { const s = el('span', 'no-mark'); s.innerHTML = NO; return s; };
  // The control's star: an SVG in the thumbs down's own 24-unit box, so
  // the five sit in identical buttons, centred alike, on one line.
  const STAR_SVG = '<svg class="icon icon-vote-star" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 2.6l2.9 6.1 6.6.8-4.9 4.6 1.3 6.6L12 17.4l-5.9 3.3 1.3-6.6-4.9-4.6 6.6-.8z"/></svg>';

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
    const voted = el('span', 'vote-count');
    voted.append(el('strong', '', votedText(tally)), ' ', el('span', 'vote-count-word', tr('voted')));
    voted.title = tr('%d of the band’s %d core members voted', tally.voted || 0, tally.members || 0);
    voted.setAttribute('aria-label', voted.title);
    if (tally.hidden) {
      // Not voted yet (or a guest, before it's decided): the server sent
      // only how many voted.
      head.append(el('span', 'vote-hidden', opts.canVote === false ? tr('The votes show once it’s decided.') : tr('Vote to see how the others voted.')), voted);
      box.append(head);
      return;
    }
    // The no thanks first (a veto), apart; then the stars.
    if (tally.noThanks && tally.noThanks.length) {
      const no = el('span', 'vote-chip is-no');
      no.append(noIcon(), ' ', trn(tally.noThanks.length, '%d no thanks', '%d no thanks'));
      head.append(no, el('span', 'vote-sum-gap'));
    }
    head.append(meter(tally.avg, tally.avgText));
    if (tally.liked) head.append(el('span', 'vote-chip is-liked', tr('The band likes it')));
    if (tally.ratings) {
      head.append(el('strong', 'vote-avg', tally.avgText));
      const split = el('span', 'vote-split');
      split.append(el('span', tally.positive ? 'is-for' : '', tr('%d for', tally.positive)), ' · ', el('span', tally.negative ? 'is-against' : '', tr('%d against', tally.negative)));
      head.append(split);
    } else {
      head.append(el('span', 'vote-none', tr('No stars yet')));
    }
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
      const chip = el('span', 'vote-chip is-no');
      chip.append(noIcon(), ' ', tr('No thanks'));
      p.append(chip, ' ', el('span', 'vote-names', tally.noThanks.map((b) => b.name).join(', ')));
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
      const b = el('button', 'vote-star');
      b.innerHTML = STAR_SVG;
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
    // No thanks first — a veto —, then a gap, then the stars: a thumbs
    // down drawn as one of them (the same button, size, states and focus
    // ring), its words in its aria-label, its title, and the line under
    // the control on hover or focus.
    const no = el('button', 'vote-star vote-nothanks');
    no.type = 'button';
    no.setAttribute('aria-label', tr('No thanks'));
    no.title = tr('No thanks: a veto');
    no.innerHTML = NO;
    no.addEventListener('click', () => cast(mine && mine.noThanks ? { clear: true } : { noThanks: true }));
    no.addEventListener('pointerenter', () => preview(0, true));
    no.addEventListener('focus', () => preview(0, true));
    no.addEventListener('pointerleave', () => preview(0));
    no.addEventListener('blur', () => preview(0));
    row.append(no, el('span', 'vote-ctl-gap'), stars);
    const foot = el('div', 'vote-ctl-foot');
    const said = el('span', 'vote-said');
    const clear = el('button', 'link-button vote-clear', tr('Take back my vote'));
    clear.type = 'button';
    clear.addEventListener('click', () => cast({ clear: true }));
    foot.append(said, clear);
    const status = el('p', 'form-status vote-status');
    status.setAttribute('role', 'status');
    box.append(row, foot, status);

    function preview(n, refusing) {
      const shown = n || (mine && mine.stars) || 0;
      buttons.forEach((b, i) => b.classList.toggle('is-on', i < shown));
      stars.classList.toggle('is-preview', !!n);
      no.classList.toggle('is-preview', !!refusing && !(mine && mine.noThanks));
      said.textContent = refusing ? tr('No thanks: a veto') : n ? tr(meaning(n)) : mine ? (mine.noThanks ? tr('You said no thanks') : tr('Your vote: %s', tr(meaning(mine.stars)))) : tr('Not voted yet');
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
        Forms.status(status, v.clear ? tr('Vote taken back: the others’ votes are hidden again.') : tr('Saved.'), 'ok');
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
      const flag = (ty) => card.classList.toggle('is-vetoed', !!(ty && ty.vetoed));
      card._tally = tally;
      flag(tally);
      summary(sum, tally, { status, full, canVote: !!card.querySelector('[data-ctl]') || status !== 'open' });
      const ctlBox = card.querySelector('[data-ctl]'); // a core member's only
      if (ctlBox && status === 'open') {
        control(ctlBox, {
          bandId, proposalId, title: card.dataset.title, mine: tally && tally.mine, rule: card.dataset.rule,
          onVoted: (a) => {
            card._tally = a.tally;
            flag(a.tally);
            summary(sum, a.tally, { status: a.status, full });
            if (a.status !== 'open') {
              card.dataset.status = a.status;
              ctlBox.replaceChildren(el('p', 'vote-decided is-' + a.status, a.status === 'accepted' ? tr('Accepted: it’s in the songbook.') : tr('Turned down by the votes.')));
            }
          },
        });
      }
    }
    decide();
  }

  // A core member's decision by hand, whatever the votes: a confirm says
  // who said no thanks (when the decider sees the votes), then it's done.
  function decide() {
    document.querySelectorAll('.vote-decide [data-decide]').forEach((btn) => {
      btn.addEventListener('click', async () => {
        const box = btn.closest('.vote-decide');
        const card = document.querySelector(`.vote-card[data-proposal="${box.dataset.proposal}"]`);
        const ty = card && card._tally;
        const title = box.dataset.title || tr('this song');
        let ask;
        if (btn.dataset.decide === 'reject') ask = tr('Turn down “%s”?', title);
        else if (ty && ty.hidden) ask = tr('You haven’t voted: the others’ votes show once you vote. %s anyway?', btn.textContent.trim());
        else if (ty && ty.noThanks && ty.noThanks.length) ask = tr('%s said no thanks. %s anyway?', ty.noThanks.map((b) => b.name).join(', '), btn.textContent.trim());
        else ask = tr('%s: “%s”?', btn.textContent.trim(), title);
        if (!confirm(ask)) return;
        const st = box.querySelector('.form-status');
        Forms.status(st, '');
        Forms.busy(btn, true);
        const res = await fetch(`/api/bands/${box.dataset.band}/proposals/${box.dataset.proposal}/${btn.dataset.decide}`, { method: 'POST' }).catch(() => null);
        if (res && res.ok) { location.reload(); return; }
        const data = res ? await res.json().catch(() => ({})) : {};
        Forms.busy(btn, false);
        Forms.status(st, data.message || tr('Couldn’t save that. Try again.'), 'error');
      });
    });
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
        s.append(el('span', 'pl-score-avg', ty.ratings ? `${STAR} ${ty.avgText}` : `${STAR} –`), el('span', 'pl-score-count', votedText(ty)));
        if (ty.mine) {
          const m = el('span', 'pl-score-mine');
          if (ty.mine.noThanks) { m.append(noIcon()); m.setAttribute('aria-label', tr('You said no thanks')); } else m.textContent = STAR.repeat(ty.mine.stars);
          s.append(m);
        }
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
          bandId: opts.bandId, proposalId: track.proposal, title: track.title, mine: ty && ty.mine, rule: opts.rule,
          onVoted: (a) => { tallies[id] = a.tally; summary(panel, a.tally, { status: a.status, full: true, canVote: opts.canVote }); scores(); },
        });
        bar.append(el('p', 'vote-keys hint', tr('Keys: 1 to 4 stars, N for no thanks')));
      } else {
        bar.replaceChildren(el('p', 'vote-guest-note', tr('You’re a guest member: you listen along; the core members vote.')));
      }
      if (panel) summary(panel, ty, { status: 'open', full: true, canVote: opts.canVote });
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
    }
    document.addEventListener('playlist:track', (e) => show(e.detail.track));
    // Others' votes come in: the one playing's, every few seconds.
    setInterval(async () => {
      if (!current || document.hidden) return;
      const a = await tallyOf(opts.bandId, current.proposal);
      if (!a) return;
      tallies[String(current.proposal)] = a.tally;
      if (panel) summary(panel, a.tally, { status: a.status, full: true, canVote: opts.canVote });
      scores();
    }, 6000);
    keys(() => ctl);
    scores();
    if (root.SongbooksPlaylist) show(root.SongbooksPlaylist.track());
  }

  root.BandVotes = { control, summary, list, decide, player, keys, ...logic };
})(typeof window !== 'undefined' ? window : globalThis);
