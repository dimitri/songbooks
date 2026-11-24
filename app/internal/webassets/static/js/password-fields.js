// Password fields on the login, invite and profile pages.
//
// Show/hide toggle:
//   <div class="pw-field">
//     <input type="password" id="X" ...>
//     <button type="button" class="pw-toggle" aria-controls="X [Y]" aria-pressed="false" hidden>
//       <span class="pw-toggle-text">Show</span><span class="sr-only"> password</span></button>
//   </div>
// The button ships hidden so it only appears when this script runs. One
// toggle can reveal several fields (a password and its confirmation), so
// people see both at once. Typing a password on a phone is error-prone;
// seeing it beats retyping it.
//
// Confirmation: <input type="password" data-match="X"> fails the form's
// own validation until it equals #X, so forms.js shows the mismatch under
// the field before anything is sent.
//
// A new account's refusal: newAccountFieldError(reason, {email, password,
// confirm}) puts a server reason about one field (password_rule.go's, or
// "enter a valid email address") under that field, by id, and says
// whether it did. The invite, join and event pages share it.
(function () {
  const t = window.t || ((m) => m);
  const reasons = [
    [/valid email/, 'email', () => t('Enter an email address like name@example.com.')],
    [/password is required/, 'password', () => t('Choose a password of at least %d characters.', 8)],
    [/at least 8/, 'password', () => t('Use at least %d characters.', 8)],
    [/too long/, 'password', () => t('That password is too long. Use a shorter one.')],
    [/didn't match/, 'confirm', () => t('The two passwords didn’t match. Type the same one in both.')],
  ];
  window.newAccountFieldError = function (reason, ids) {
    for (const [re, key, text] of reasons) {
      const input = ids[key] && document.getElementById(ids[key]);
      if (re.test(reason) && input && window.Forms) {
        Forms.fieldError(input, text());
        input.focus();
        return true;
      }
    }
    return false;
  };

  document.querySelectorAll('button.pw-toggle[aria-controls]').forEach((btn) => {
    const inputs = btn.getAttribute('aria-controls').split(/\s+/)
      .map((id) => document.getElementById(id)).filter(Boolean);
    if (!inputs.length) return;
    const label = btn.querySelector('.pw-toggle-text') || btn;
    btn.hidden = false;
    function set(shown) {
      inputs.forEach((i) => { i.type = shown ? 'text' : 'password'; });
      btn.setAttribute('aria-pressed', shown ? 'true' : 'false');
      label.textContent = shown ? t('Hide') : t('Show');
    }
    btn.addEventListener('click', () => {
      set(inputs[0].type === 'password');
      inputs[0].focus();
    });
    // Back to dots before the form goes, so the browser offers to save it
    // as a password and nothing lingers on screen during the redirect.
    if (inputs[0].form) inputs[0].form.addEventListener('submit', () => set(false));
  });

  document.querySelectorAll('input[data-match]').forEach((confirm) => {
    const pw = document.getElementById(confirm.dataset.match);
    if (!pw) return;
    function check() {
      const bad = confirm.value !== '' && confirm.value !== pw.value;
      confirm.setCustomValidity(bad ? t('The two passwords don’t match. Type the same one in both.') : '');
      if (!bad && confirm.getAttribute('aria-invalid') === 'true' && window.Forms) Forms.fieldError(confirm, '');
    }
    pw.addEventListener('input', check);
    confirm.addEventListener('input', check);
  });
})();
