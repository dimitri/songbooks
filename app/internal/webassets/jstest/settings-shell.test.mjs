// The settings shell (static/js/settings-shell.js): one section at a
// time by the URL's #hash — a wide screen opens on the page's default
// section, else the first; a phone on the list. Run by webassets'
// TestScripts (go test), or alone, from internal/webassets:
// node --test 'jstest/*.test.mjs'
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const S = require('../static/js/settings-shell.js');

const ids = ['band', 'look', 'roles', 'danger'];

test('the #hash picks its section', () => {
  assert.equal(S.pick('#roles', ids, false), 'roles');
  assert.equal(S.pick('#roles', ids, true), 'roles');
  assert.equal(S.pick('roles', ids, false), 'roles');
});

test('no #hash: the first section on a wide screen, the list on a phone', () => {
  assert.equal(S.pick('', ids, false), 'band');
  assert.equal(S.pick('', ids, true), '');
  assert.equal(S.pick('#', ids, true), '');
});

test('the page may open on another section (an archived band: danger)', () => {
  assert.equal(S.pick('', ids, false, 'danger'), 'danger');
  assert.equal(S.pick('#look', ids, false, 'danger'), 'look');
  assert.equal(S.pick('', ids, false, 'nowhere'), 'band');
  assert.equal(S.pick('', ids, true, 'danger'), '');
});

test('an unknown or broken #hash falls back', () => {
  assert.equal(S.pick('#members', ids, false), 'band');
  assert.equal(S.pick('#%E0%A4%A', ids, false), 'band');
  assert.equal(S.pick('#members', ids, true), '');
  assert.equal(S.pick('', [], false), '');
});

// A layout as small as the script needs: sections, links, classes.
function el(attrs = {}) {
  const classes = new Set();
  const a = { ...attrs };
  return {
    ...attrs,
    hidden: false,
    dataset: attrs.dataset || {},
    classList: {
      add: (c) => classes.add(c),
      toggle: (c, on) => (on ? classes.add(c) : classes.delete(c)),
      contains: (c) => classes.has(c),
    },
    setAttribute: (k, v) => { a[k] = v; },
    removeAttribute: (k) => { delete a[k]; },
    getAttribute: (k) => a[k],
  };
}

test('wire shows one section, marks its link, follows the #hash', () => {
  const sections = ids.map((id) => el({ id }));
  const links = ids.map((id) => el({ dataset: { sectionLink: id } }));
  const layout = el({ dataset: { defaultSection: '' } });
  layout.querySelectorAll = (sel) => (sel === '.admin-section' ? sections : links);
  const handlers = {};
  const win = {
    location: { hash: '' },
    addEventListener: (ev, fn) => { handlers[ev] = fn; },
    scrollTo: () => {},
  };
  const media = { matches: false, addEventListener: () => {} };
  S.wire(layout, media, win);
  assert.deepEqual(sections.map((s) => s.hidden), [false, true, true, true]);
  assert.equal(links[0].getAttribute('aria-current'), 'page');
  assert.ok(layout.classList.contains('has-section'));

  win.location.hash = '#danger';
  handlers.hashchange();
  assert.deepEqual(sections.map((s) => s.hidden), [true, true, true, false]);
  assert.equal(links[0].getAttribute('aria-current'), undefined);
  assert.equal(links[3].getAttribute('aria-current'), 'page');

  // A phone, back to the list: every section hidden, the list shows.
  media.matches = true;
  win.location.hash = '';
  handlers.hashchange();
  assert.ok(sections.every((s) => s.hidden));
  assert.ok(!layout.classList.contains('has-section'));
});
