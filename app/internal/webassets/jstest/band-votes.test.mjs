// A band's votes' plain logic (static/js/band-votes.js): the keys that
// vote, what each star says, the average's meter, the voted count, what
// is sent. Run by webassets' TestScripts (go test), or alone, from
// internal/webassets: node --test 'jstest/*.test.mjs'
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const BV = require('../static/js/band-votes.js');

test('keys 1 to 4 give stars, N no thanks, nothing else votes', () => {
  assert.deepEqual(BV.voteForKey('1'), { stars: 1 });
  assert.deepEqual(BV.voteForKey('4'), { stars: 4 });
  assert.deepEqual(BV.voteForKey('n'), { noThanks: true });
  assert.deepEqual(BV.voteForKey('N'), { noThanks: true });
  for (const k of ['0', '5', '9', 'x', ' ', 'Enter']) assert.equal(BV.voteForKey(k), null, k);
});

test('an even scale: 1 and 2 against, 3 and 4 for', () => {
  assert.equal(BV.MAX, 4);
  assert.equal(BV.meaning(1), 'Not for us');
  assert.equal(BV.meaning(2), 'Rather not');
  assert.equal(BV.meaning(3), 'Yes, let’s');
  assert.equal(BV.meaning(4), 'Love it');
  assert.equal(BV.meaning(0), '');
});

test('the average fills the 4 stars', () => {
  assert.equal(BV.fill(4), 100);
  assert.equal(BV.fill(2), 50);
  assert.equal(BV.fill(3.25), 81.3);
  assert.equal(BV.fill(0), 0);
  assert.equal(BV.fill(9), 100);
});

test('voted of the core members', () => {
  assert.equal(BV.votedText({ voted: 3, members: 4 }), '3/4');
  assert.equal(BV.votedText({}), '0/0');
});

test('what a vote sends', () => {
  assert.deepEqual(BV.body({ stars: 3 }), { stars: 3 });
  assert.deepEqual(BV.body({ noThanks: true }), { noThanks: true });
  assert.deepEqual(BV.body({ clear: true }), { clear: true });
});

test('a key typed in a field doesn\'t vote', () => {
  assert.ok(BV.typing({ tagName: 'INPUT' }));
  assert.ok(BV.typing({ tagName: 'TEXTAREA' }));
  assert.ok(BV.typing({ isContentEditable: true }));
  assert.ok(!BV.typing({ tagName: 'BUTTON' }));
  assert.ok(!BV.typing(null));
});
