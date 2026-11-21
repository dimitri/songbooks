// A site admin's "Login link" for one member (templates/login_link.html,
// internal/httpapi/admin_login_link.go). Clicking a row's button opens a
// dialog: show a fresh link as a QR code and a copyable URL (to send it,
// or to hold the screen up at an event: "Full screen"), or email it to
// the member so the admin never sees it. A link works once, for 15
// minutes; the dialog counts down, notices when it has been used, and
// offers a new one.
(function () {
  const STATUS_EVERY_MS = 4000;

  let dlg = null;     // the <dialog>, built on first use
  let person = null;  // {id, name}
  let link = null;    // the server's answer for the link on screen
  let deadline = 0;   // performance.now() when it expires
  let tick = null, poll = null;

  const $ = (sel) => dlg.querySelector(sel);

  function build() {
    dlg = document.createElement('dialog');
    dlg.className = 'login-link-dialog';
    dlg.setAttribute('aria-labelledby', 'login-link-title');
    dlg.innerHTML = `
      <div class="ll-head">
        <h2 id="login-link-title"></h2>
        <button type="button" class="icon-btn ll-close"><span aria-hidden="true">×</span></button>
      </div>
      <div class="ll-start">
        <p class="ll-start-text"></p>
        <div class="form-actions">
          <button type="button" class="primary ll-show"></button>
          <button type="button" class="ll-email"></button>
        </div>
      </div>
      <div class="ll-link" hidden>
        <div class="ll-qr-wrap">
          <div class="ll-qr" role="img"></div>
          <p class="ll-qr-state" hidden></p>
        </div>
        <p class="ll-who"></p>
        <p class="ll-countdown" aria-live="off"></p>
        <div class="ll-url-row">
          <label class="sr-only" for="ll-url"></label>
          <input type="text" id="ll-url" readonly spellcheck="false">
          <button type="button" class="ll-copy"></button>
        </div>
        <p class="hint ll-rules"></p>
        <p class="hint ll-one"></p>
        <p class="ll-admin-warn" hidden></p>
        <div class="form-actions ll-actions">
          <button type="button" class="ll-full"></button>
          <button type="button" class="ll-new"></button>
          <button type="button" class="ll-email ll-email-instead"></button>
        </div>
        <button type="button" class="ll-exit-full"></button>
      </div>
      <p class="form-status ll-status" role="status"></p>`;
    $('.ll-close').setAttribute('aria-label', t('Close'));
    $('.ll-show').textContent = t('Show the link and QR code');
    $('.ll-start .ll-email').textContent = t('Email it to them');
    $('.ll-email-instead').textContent = t('Send it by email instead');
    $('label[for="ll-url"]').textContent = t('Login link');
    $('.ll-copy').textContent = t('Copy');
    $('.ll-full').textContent = t('Full screen');
    $('.ll-new').textContent = t('New link');
    $('.ll-exit-full').textContent = t('Exit full screen');
    $('.ll-qr').setAttribute('aria-label', t('QR code of the login link'));

    $('.ll-close').addEventListener('click', () => dlg.close());
    $('.ll-show').addEventListener('click', (e) => create(e.currentTarget));
    $('.ll-new').addEventListener('click', (e) => create(e.currentTarget));
    dlg.querySelectorAll('.ll-email').forEach(b => b.addEventListener('click', () => sendEmail(b)));
    $('.ll-copy').addEventListener('click', copy);
    $('#ll-url').addEventListener('focus', (e) => e.target.select());
    $('.ll-full').addEventListener('click', () => full(true));
    $('.ll-exit-full').addEventListener('click', () => full(false));
    // Esc in full screen leaves full screen, not the dialog.
    dlg.addEventListener('cancel', (e) => {
      if (dlg.classList.contains('is-full')) { e.preventDefault(); full(false); }
    });
    document.addEventListener('fullscreenchange', () => {
      if (!document.fullscreenElement && dlg.classList.contains('is-full')) full(false);
    });
    dlg.addEventListener('close', () => { stop(); full(false); link = null; });
    document.body.appendChild(dlg);
  }

  function open(btn) {
    if (!dlg) build();
    person = {id: btn.dataset.personId, name: btn.dataset.name};
    link = null;
    stop();
    $('#login-link-title').textContent = t('Login link for %s', person.name);
    $('.ll-start-text').textContent = t('A link that signs %s in on any phone or computer, without a password. It works once, for 15 minutes.', person.name);
    $('.ll-start').hidden = false;
    $('.ll-link').hidden = true;
    $('.ll-qr').innerHTML = '';
    $('#ll-url').value = '';
    Forms.status($('.ll-status'), '', '');
    dlg.showModal();
    $('.ll-show').focus();
  }

  const api = (path, body) => fetch(`/api/admin/people/${person.id}/login-link${path}`, {
    method: 'POST', headers: {'Content-Type': 'application/json'},
    body: body ? JSON.stringify(body) : undefined,
  });

  async function create(btn) {
    Forms.busy(btn, true);
    Forms.status($('.ll-status'), '', '');
    try {
      const res = await api('');
      if (res.status === 409) throw new Error(t('%s hasn’t made an account yet: send them their invite link instead.', person.name));
      if (!res.ok) throw new Error(t('Couldn’t make a link. Try again.'));
      show(await res.json());
    } catch (err) {
      Forms.status($('.ll-status'), err.message, 'error');
    } finally {
      Forms.busy(btn, false);
    }
  }

  function show(data) {
    link = data;
    deadline = performance.now() + data.secondsLeft * 1000;
    $('.ll-start').hidden = true;
    $('.ll-link').hidden = false;
    $('.ll-link').classList.remove('is-over');
    $('.ll-qr').innerHTML = data.qrSvg; // drawn by the server, no user text in it
    $('.ll-qr-state').hidden = true;
    $('.ll-who').textContent = data.name;
    $('#ll-url').value = data.url;
    $('.ll-rules').textContent = t('Valid 15 minutes, works once. Whoever opens it is signed in as %s.', data.name);
    $('.ll-one').textContent = t('One QR code, one person: once it’s scanned it stops working. Make a new link for each phone.');
    $('.ll-admin-warn').hidden = !data.isAdmin;
    $('.ll-admin-warn').textContent = t('%s is a site admin: this link signs in with admin rights.', data.name);
    $('.ll-email-instead').hidden = !data.hasEmail;
    $('.ll-new').classList.remove('primary');
    $('.ll-full').disabled = false;
    $('.ll-copy').disabled = false;
    stop();
    countdown();
    tick = setInterval(countdown, 1000);
    poll = setInterval(checkUsed, STATUS_EVERY_MS);
    $('.ll-copy').focus();
  }

  function stop() {
    clearInterval(tick); clearInterval(poll);
    tick = poll = null;
  }

  function countdown() {
    const left = Math.max(0, Math.round((deadline - performance.now()) / 1000));
    if (left === 0) { over(t('Expired. Make a new link.'), t('Expired')); return; }
    const mm = Math.floor(left / 60), ss = String(left % 60).padStart(2, '0');
    $('.ll-countdown').textContent = t('Expires in %s', `${mm}:${ss}`);
  }

  async function checkUsed() {
    if (!link || document.hidden) return;
    const res = await api('/status', {token: link.token}).catch(() => null);
    if (!res || !res.ok) return;
    const st = await res.json();
    if (st.used) over(t('Used: %s is signed in. Make a new link for another phone.', link.name), t('Used'));
    else deadline = performance.now() + st.secondsLeft * 1000;
  }

  // The link on screen no longer works (used or expired): say so over
  // the QR code, so nobody scans a dead one, and offer a new one.
  function over(msg, short) {
    stop();
    $('.ll-link').classList.add('is-over');
    $('.ll-qr-state').hidden = false;
    $('.ll-qr-state').textContent = msg;
    $('.ll-countdown').textContent = short;
    $('.ll-copy').disabled = true;
    $('.ll-full').disabled = true;
    full(false);
    $('.ll-new').classList.add('primary');
    if (link) link.over = true;
  }

  async function sendEmail(btn) {
    Forms.busy(btn, true);
    Forms.status($('.ll-status'), '', '');
    try {
      // The link on screen stops working: only the emailed one will.
      const res = await api('/email', link && !link.over ? {revoke: link.token} : null);
      if (res.status === 409) throw new Error(t('%s has no email address on their account.', person.name));
      if (!res.ok) throw new Error(t('Couldn’t send the email. Try again.'));
      stop();
      link = null;
      full(false);
      $('.ll-link').hidden = true;
      $('.ll-qr').innerHTML = '';
      $('#ll-url').value = '';
      $('.ll-start').hidden = false;
      Forms.status($('.ll-status'), t('Sent. %s has a login link in their email, valid 15 minutes.', person.name), 'ok');
    } catch (err) {
      Forms.status($('.ll-status'), err.message, 'error');
    } finally {
      Forms.busy(btn, false);
    }
  }

  function copy() {
    const btn = $('.ll-copy');
    const show = (text) => { btn.textContent = text; setTimeout(() => { btn.textContent = t('Copy'); }, 1500); };
    navigator.clipboard.writeText($('#ll-url').value).then(() => show(t('Copied')), () => {
      $('#ll-url').select();
      show(t('Couldn’t copy'));
    });
  }

  // Full screen: the QR code as big as the screen allows, black on
  // white whatever the theme, the member's name and the countdown under
  // it — to hold up at arm's length. The browser's own full screen when
  // it has one (not on an iPhone), the whole window either way.
  function full(on) {
    if (!dlg) return;
    dlg.classList.toggle('is-full', on);
    if (on) {
      if (dlg.requestFullscreen) dlg.requestFullscreen().catch(() => {});
      $('.ll-exit-full').focus();
    } else if (document.fullscreenElement === dlg) {
      document.exitFullscreen().catch(() => {});
    }
  }

  document.addEventListener('click', (e) => {
    const btn = e.target.closest('.login-link-btn');
    if (btn) open(btn);
  });
})();
