// Shared form behaviour (see "Form conventions" in app.css).
//
// Any <form> on the page keeps the browser's constraint checks (required,
// type="email", min, pattern...) before its submit handler runs, but each
// failure is shown under its field in the page's own words and style
// instead of the browser's bubble, and focus goes to the first field to
// fix. A form opts out with data-novalidate-js. Fields outside a <form>
// get the same with Forms.validate(container).
//
// Page scripts use the helpers on window.Forms:
//   Forms.fieldError(input, 'message')  — '' clears it
//   Forms.status(form or element, 'message', 'error' | 'ok' | '')
//   Forms.busy(button, true/false)       — spinner, disabled, aria-busy
//   await Forms.run(form, button, async () => { ... })
//                                        — busy while it runs; a thrown
//                                          Error's message lands in the
//                                          form's .form-status as an error
(function () {
  // The page's language (/i18n/{lang}.js, loaded first); English without it.
  const t = window.t || function (m) { const a = [].slice.call(arguments, 1); let i = 0; return m.replace(/%[ds]/g, () => String(a[i++])); };

  function errorEl(input) {
    const id = (input.id || input.name || 'field') + '-error';
    let el = document.getElementById(id);
    if (!el) {
      el = document.createElement('p');
      el.className = 'field-error';
      el.id = id;
      // Under the control, or under the group for a radio/checkbox set,
      // or under a password field's row (the field and its Show button).
      const anchor = input.closest('fieldset.field-group') || input.closest('label.check') || input.closest('.pw-field') || input;
      anchor.insertAdjacentElement('afterend', el);
    }
    const ids = (input.getAttribute('aria-describedby') || '').split(/\s+/).filter(Boolean);
    if (!ids.includes(id)) { ids.push(id); input.setAttribute('aria-describedby', ids.join(' ')); }
    return el;
  }

  function fieldError(input, msg) {
    if (!msg) {
      input.removeAttribute('aria-invalid');
      const el = document.getElementById((input.id || input.name || 'field') + '-error');
      if (el) el.textContent = '';
      return;
    }
    input.setAttribute('aria-invalid', 'true');
    errorEl(input).textContent = msg;
  }

  function labelText(input) {
    const lab = (input.id && document.querySelector('label[for="' + CSS.escape(input.id) + '"]')) || input.closest('label');
    const text = lab ? lab.textContent : (input.getAttribute('aria-label') || '');
    return text.replace(/\(optional\)/i, '').replace(t('(optional)'), '').replace(/\s+/g, ' ').trim();
  }

  // The message under a field, in plain words rather than the browser's.
  function message(input) {
    const v = input.validity, name = labelText(input);
    if (v.valueMissing) {
      if (input.type === 'checkbox') return t('Tick this box to continue.');
      if (input.tagName === 'SELECT' || input.type === 'radio') return name ? t('Choose %s.', name.toLowerCase()) : t('Choose one.');
      return name ? t('Enter %s.', name.toLowerCase()) : t('This can’t be empty.');
    }
    if (v.typeMismatch && input.type === 'email') return t('Enter an email address like name@example.com.');
    if (v.typeMismatch && input.type === 'url') return t('Enter a full web address, starting with https://.');
    if (v.rangeUnderflow) return t('Enter %s or more.', input.min);
    if (v.rangeOverflow) return t('Enter %s or less.', input.max);
    if (v.tooShort) return t('Use at least %d characters.', input.minLength);
    if (v.stepMismatch || v.badInput) return t('Enter a whole number.');
    if (v.patternMismatch && input.title) return input.title;
    return input.validationMessage;
  }

  function validate(scope) {
    let first = null;
    for (const el of scope.querySelectorAll('input, select, textarea')) {
      if (!el.willValidate || el.type === 'submit' || el.type === 'button') continue;
      if (el.checkValidity()) { if (el.getAttribute('aria-invalid')) fieldError(el, ''); continue; }
      fieldError(el, message(el));
      first = first || el;
    }
    if (first) first.focus();
    return !first;
  }

  function statusEl(scope) {
    if (scope.classList && scope.classList.contains('form-status')) return scope;
    let el = scope.querySelector('.form-status');
    if (!el) {
      el = document.createElement('p');
      el.className = 'form-status';
      el.setAttribute('role', 'status');
      scope.appendChild(el);
    }
    return el;
  }

  function status(scope, msg, kind) {
    const el = statusEl(scope);
    el.textContent = msg || '';
    el.classList.toggle('is-error', kind === 'error');
    el.classList.toggle('is-ok', kind === 'ok');
  }

  function busy(btn, on) {
    if (!btn) return;
    btn.disabled = on;
    btn.classList.toggle('btn-loading', on);
    if (on) {
      btn.setAttribute('aria-busy', 'true');
      if (!btn.querySelector('.spinner')) btn.insertAdjacentHTML('beforeend', '<span class="spinner" aria-hidden="true"></span>');
    } else {
      btn.removeAttribute('aria-busy');
      btn.querySelector('.spinner')?.remove();
    }
  }

  async function run(form, btn, fn) {
    status(form, '');
    busy(btn, true);
    try { return await fn(); } catch (e) {
      status(form, (e && e.message) || t('That didn’t work. Try again.'), 'error');
    } finally { busy(btn, false); }
  }

  // The browser checks a form before it fires submit, and fires `invalid`
  // on each field that fails: cancelling it drops the browser's bubble,
  // and the message goes under the field instead. The first one of the
  // batch takes focus.
  let focusing = false;
  document.addEventListener('invalid', (e) => {
    const el = e.target;
    if (el.form && el.form.hasAttribute('data-novalidate-js')) return;
    e.preventDefault();
    fieldError(el, message(el));
    if (!focusing) { focusing = true; el.focus(); queueMicrotask(() => { focusing = false; }); }
  }, true);
  // A field's error clears as soon as it's fixed.
  document.addEventListener('input', (e) => {
    const el = e.target;
    if (el.getAttribute && el.getAttribute('aria-invalid') === 'true' && el.checkValidity()) fieldError(el, '');
  });
  document.addEventListener('change', (e) => {
    const el = e.target;
    if (el.getAttribute && el.getAttribute('aria-invalid') === 'true' && el.checkValidity()) fieldError(el, '');
  });
  // An add answered 409 (httpapi writeLinkErr): another band's private
  // song, which a band doesn't link, or a closed songbook's. With the
  // work's catalogue version, asks to add that instead: {retryId} on yes;
  // otherwise {message} to show, or {} when the person said no.
  async function bandRefusal(res) {
    const d = await res.json().catch(() => ({}));
    if (d.catalogVariantId) return confirm(d.question) ? { retryId: d.catalogVariantId } : {};
    return { message: d.message || '' };
  }
  window.Forms = { fieldError, status, busy, run, validate, bandRefusal };
})();
