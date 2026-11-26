// Albums and live versions (albums.html, internal/httpapi/albums.go): the
// new album dialog, an album's editor (its facts, artwork and tracks), its
// Public switch, "Make the live version", "Use the live versions here",
// and the band's "Set lists use" setting. Loaded once per page (several
// templates may include it).
(() => {
  if (window.__albumsJS) return;
  window.__albumsJS = true;
  const T = (s, ...a) => (window.t ? window.t(s, ...a) : s);
  const status = (el, msg, kind) => { if (window.Forms && el) Forms.status(el, msg, kind); else if (el) el.textContent = msg; };
  const busy = (el, on) => { if (window.Forms && el) Forms.busy(el, on); else if (el) el.disabled = on; };
  const json = async (method, url, body) => {
    const res = await fetch(url, {
      method, headers: body ? {'Content-Type': 'application/json'} : {}, body: body ? JSON.stringify(body) : undefined,
    }).catch(() => null);
    const data = res ? await res.json().catch(() => ({})) : {};
    return {ok: !!(res && res.ok), status: res ? res.status : 0, data};
  };
  const closeOn = (dialog) => dialog?.querySelectorAll('[data-close]').forEach((b) => b.addEventListener('click', () => dialog.close()));

  // ---- New album (the band's Albums page).
  const newDialog = document.getElementById('album-new-dialog');
  if (newDialog) {
    closeOn(newDialog);
    document.getElementById('album-new-open')?.addEventListener('click', () => newDialog.showModal());
    const form = document.getElementById('album-new-form');
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      const btn = form.querySelector('[type=submit]');
      busy(btn, true);
      const r = await json('POST', `/api/bands/${form.dataset.bandId}/albums`, {title: form.title.value, kind: form.kind.value});
      if (r.ok) { location.href = r.data.href + '?edit=1'; return; }
      busy(btn, false);
      status(form.querySelector('.form-status'), r.status === 409 ? T('The band has a release of that name already.') : T('Couldn’t create the album. Try again.'), 'error');
    });
  }

  // ---- An album's editor.
  const ed = window.albumEditor;
  const editDialog = document.getElementById('album-edit-dialog');
  if (ed && editDialog) {
    closeOn(editDialog);
    const $ = (id) => document.getElementById(id);
    const form = $('album-edit-form');
    const list = $('album-edit-tracks');
    const coverImg = $('album-edit-cover-img');
    const coverRemove = $('album-edit-cover-remove');
    const showCover = (src) => { coverImg.hidden = !src; if (src) coverImg.src = src; coverRemove.hidden = !src; };
    const mmss = (s) => (s ? `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}` : '');
    const secs = (v) => {
      const m = String(v || '').trim().match(/^(\d+):(\d{1,2})$/);
      if (m) return parseInt(m[1], 10) * 60 + parseInt(m[2], 10);
      const n = parseInt(v, 10);
      return Number.isFinite(n) ? n : 0;
    };
    const option = (value, label, selected) => { const o = document.createElement('option'); o.value = value; o.textContent = label; o.selected = selected; return o; };
    const input = (cls, value, attrs = {}) => {
      const i = document.createElement('input');
      i.className = cls; i.value = value ?? '';
      for (const [k, v] of Object.entries(attrs)) i.setAttribute(k, v);
      return i;
    };
    const masterLabel = (m) => (m.path ? m.path.split('/').slice(-1)[0] + ' / ' : '') + m.name;
    const trackRow = (t) => {
      const li = document.createElement('li');
      li.className = 'album-edit-track';
      const title = input('album-edit-t-title', t.title, {'aria-label': T('Title as printed'), placeholder: T('Title as printed'), maxlength: '200'});
      const song = document.createElement('select');
      song.className = 'album-edit-t-song'; song.setAttribute('aria-label', T('The band’s song'));
      song.append(option('0', T('No song (an interlude, a hidden track)'), !t.songId));
      for (const s of ed.songs || []) song.append(option(s.id, s.title, s.id === t.songId));
      song.addEventListener('change', () => {
        if (!title.value) title.value = song.selectedOptions[0]?.textContent || '';
      });
      const master = document.createElement('select');
      master.className = 'album-edit-t-master'; master.setAttribute('aria-label', T('Its master, in the band’s folder'));
      master.append(option('0', T('No master'), !t.recordingId));
      for (const m of ed.masters || []) master.append(option(m.id, masterLabel(m), m.id === t.recordingId));
      const dur = input('album-edit-t-dur', mmss(t.durationS), {'aria-label': T('Duration'), placeholder: '3:50', inputmode: 'numeric', size: '5'});
      const tempo = input('album-edit-t-tempo', t.tempo || '', {'aria-label': T('Tempo'), placeholder: T('bpm'), inputmode: 'numeric', size: '4'});
      const isrc = input('album-edit-t-isrc', t.isrc, {'aria-label': 'ISRC', placeholder: 'ISRC', size: '15', spellcheck: 'false'});
      const tools = document.createElement('span');
      tools.className = 'album-edit-t-tools';
      for (const [dir, label, sym] of [['up', T('move up'), '▲'], ['down', T('move down'), '▼'], ['remove', T('Remove the track'), '✕']]) {
        const b = document.createElement('button');
        b.type = 'button'; b.className = 'icon-btn'; b.title = label; b.setAttribute('aria-label', label); b.textContent = sym;
        b.addEventListener('click', () => {
          if (dir === 'up' && li.previousElementSibling) list.insertBefore(li, li.previousElementSibling);
          else if (dir === 'down' && li.nextElementSibling) list.insertBefore(li.nextElementSibling, li);
          else if (dir === 'remove') li.remove();
        });
        tools.append(b);
      }
      li.dataset.link = t.linkUrl || '';
      li.dataset.disc = t.disc || 1;
      li.append(title, song, master, dur, tempo, isrc, tools);
      return li;
    };
    const fill = () => {
      $('album-edit-name').value = ed.title || '';
      $('album-edit-kind').value = ed.kind || 'album';
      $('album-edit-date').value = ed.releasedOn || '';
      $('album-edit-label').value = ed.label || '';
      $('album-edit-credits').value = ed.credits || '';
      $('album-edit-licence').value = ed.licence || '';
      $('album-edit-links').value = (ed.links || []).map((l) => l.url).join('\n');
      list.replaceChildren(...(ed.tracks || []).map(trackRow));
      showCover(ed.cover);
    };
    $('album-edit-add').addEventListener('click', () => {
      const li = trackRow({});
      list.append(li);
      li.querySelector('select')?.focus();
    });
    $('album-edit-open')?.addEventListener('click', () => { fill(); editDialog.showModal(); });
    if (new URLSearchParams(location.search).get('edit') === '1') { fill(); editDialog.showModal(); }

    $('album-edit-cover-file').addEventListener('change', async (e) => {
      const file = e.target.files[0];
      if (!file) return;
      const fd = new FormData();
      fd.append('file', file);
      status(form.querySelector('.form-status'), T('Sending the artwork…'), '');
      const res = await fetch(`/api/bands/${ed.bandId}/albums/${ed.id}/cover`, {method: 'PUT', body: fd}).catch(() => null);
      const data = res ? await res.json().catch(() => ({})) : {};
      if (res && res.ok) { ed.cover = data.cover; showCover(data.cover); status(form.querySelector('.form-status'), T('Artwork saved.'), 'ok'); return; }
      status(form.querySelector('.form-status'), (data && data.error) || T('Couldn’t save the artwork. Try a PNG or JPEG under 12 MB.'), 'error');
    });
    coverRemove.addEventListener('click', async () => {
      const r = await json('DELETE', `/api/bands/${ed.bandId}/albums/${ed.id}/cover`);
      if (r.ok) { ed.cover = ''; showCover(''); }
    });

    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      const btn = form.querySelector('[type=submit]');
      const tracks = [...list.children].map((li) => ({
        disc: parseInt(li.dataset.disc, 10) || 1,
        title: li.querySelector('.album-edit-t-title').value,
        songId: parseInt(li.querySelector('.album-edit-t-song').value, 10) || 0,
        recordingId: parseInt(li.querySelector('.album-edit-t-master').value, 10) || 0,
        durationS: secs(li.querySelector('.album-edit-t-dur').value),
        tempo: parseInt(li.querySelector('.album-edit-t-tempo').value, 10) || 0,
        isrc: li.querySelector('.album-edit-t-isrc').value,
        linkUrl: li.dataset.link || '',
      }));
      const links = $('album-edit-links').value.split('\n').map((u) => u.trim()).filter(Boolean).map((url) => ({kind: '', url}));
      busy(btn, true);
      const r = await json('PUT', `/api/bands/${ed.bandId}/albums/${ed.id}`, {
        title: $('album-edit-name').value, kind: $('album-edit-kind').value, releasedOn: $('album-edit-date').value,
        releaseYear: $('album-edit-date').value ? 0 : (ed.releaseYear || 0),
        label: $('album-edit-label').value, credits: $('album-edit-credits').value, licence: $('album-edit-licence').value,
        links, tracks,
      });
      if (r.ok) { location.href = location.pathname; return; }
      busy(btn, false);
      status(form.querySelector('.form-status'), (r.data && r.data.error) || T('Couldn’t save the album. Try again.'), 'error');
    });
    $('album-edit-delete').addEventListener('click', async () => {
      if (!confirm(T('Delete “%s”? Its songs stay in the songbook.', ed.title))) return;
      const r = await json('DELETE', `/api/bands/${ed.bandId}/albums/${ed.id}`);
      if (r.ok) location.href = r.data.href;
      else status(form.querySelector('.form-status'), T('Couldn’t delete the album. Try again.'), 'error');
    });
  }

  // ---- Public.
  const pub = document.getElementById('album-public');
  if (pub) {
    pub.addEventListener('change', async () => {
      const on = pub.checked;
      const songs = on && confirm(T('Make its songs public too? Their live versions stay the band’s.'));
      pub.disabled = true;
      const r = await json('POST', `/api/bands/${pub.dataset.bandId}/albums/${pub.dataset.albumId}/public`, {public: on, songs});
      pub.disabled = false;
      if (r.ok) { location.reload(); return; }
      pub.checked = !on;
      status(document.getElementById('album-public-status'), T('Couldn’t change it. Try again.'), 'error');
    });
  }

  // ---- Make the live version (a song page, a set list line).
  document.querySelectorAll('[data-make-live]').forEach((btn) => btn.addEventListener('click', async () => {
    if (!confirm(T('Make the live version of this song? It starts as a copy of the song as it is now: its chart, tempo and sync.'))) return;
    busy(btn, true);
    const r = await json('POST', `/api/songs/${btn.dataset.makeLive}/live-version`);
    if (r.ok) {
      if (btn.closest('.setlist-overview-row')) location.reload();
      else location.href = `/song/${r.data.variantId}${location.search}`;
      return;
    }
    busy(btn, false);
    alert((r.data && r.data.error) || T('Couldn’t make the live version. Try again.'));
  }));

  // ---- Use the live versions here (a set list).
  const useLive = document.getElementById('use-live-versions');
  if (useLive) {
    useLive.addEventListener('click', async () => {
      busy(useLive, true);
      const r = await json('POST', `/api/setlists/${useLive.dataset.setlist}/use-live-versions`);
      if (r.ok) { location.reload(); return; }
      busy(useLive, false);
      alert(T('Couldn’t change the set list. Try again.'));
    });
  }

  // ---- Settings › The band: Set lists use.
  const sv = document.getElementById('setlist-versions-form');
  if (sv) {
    sv.querySelectorAll('input[name=setlistVersions]').forEach((radio) => radio.addEventListener('change', async () => {
      const r = await json('POST', `/api/bands/${sv.dataset.bandId}/setlist-versions`, {value: radio.value});
      status(sv.querySelector('.form-status'), r.ok ? T('Saved.') : T('Couldn’t save it. Try again.'), r.ok ? 'ok' : 'error');
    }));
  }
})();
