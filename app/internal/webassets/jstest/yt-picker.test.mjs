// The YouTube picker's plain logic (static/js/yt-picker.js): what a link
// is, the search's address, a result's line. Run by webassets'
// TestScripts (go test), or alone, from internal/webassets:
// node --test 'jstest/*.test.mjs'
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const YT = require('../static/js/yt-picker.js');

test('a pasted link, not words', () => {
  for (const s of ['https://youtu.be/x', 'http://example.com', 'www.youtube.com/watch?v=x', 'youtu.be/x', 'youtube.com/watch?v=x', ' m.youtube.com/watch?v=x ']) assert.ok(YT.looksLink(s), s);
  for (const s of ['Wonderwall Oasis', 'Whiskey, Beer & Wine — Lazy Lester', '', null]) assert.ok(!YT.looksLink(s), String(s));
});

test('a video id from any of its addresses', () => {
  assert.equal(YT.youtubeId('https://www.youtube.com/watch?v=zLjf5P7Ef5k&t=3'), 'zLjf5P7Ef5k');
  assert.equal(YT.youtubeId('https://youtu.be/zLjf5P7Ef5k?si=abc'), 'zLjf5P7Ef5k');
  assert.equal(YT.youtubeId('youtu.be/zLjf5P7Ef5k'), 'zLjf5P7Ef5k');
  assert.equal(YT.youtubeId('https://m.youtube.com/shorts/zLjf5P7Ef5k'), 'zLjf5P7Ef5k');
  assert.equal(YT.youtubeId('https://music.youtube.com/watch?v=zLjf5P7Ef5k'), 'zLjf5P7Ef5k');
  assert.equal(YT.youtubeId('https://www.youtube-nocookie.com/embed/zLjf5P7Ef5k'), 'zLjf5P7Ef5k');
  assert.equal(YT.youtubeId('https://example.com/watch?v=zLjf5P7Ef5k'), null);
  assert.equal(YT.youtubeId('https://www.youtube.com/watch?v=<script>'), null);
  assert.equal(YT.youtubeId('not a link'), null);
});

test('the search\'s address: the song apart, words as typed, a link as itself', () => {
  assert.equal(YT.queryOf(' Le Vent ', 'Someone'), 'Le Vent Someone');
  assert.equal(YT.queryOf('Le Vent', ''), 'Le Vent');
  assert.equal(YT.findURL('Le Vent Someone', 'Le Vent', 'Someone'), '/api/youtube/find?title=Le+Vent&artist=Someone');
  assert.equal(YT.findURL('Le Vent', 'Le Vent', ''), '/api/youtube/find?title=Le+Vent');
  assert.equal(YT.findURL('le vent live', 'Le Vent', 'Someone'), '/api/youtube/find?q=le+vent+live');
  assert.equal(YT.findURL('https://youtu.be/zLjf5P7Ef5k', 'Le Vent', 'Someone'), '/api/youtube/find?url=https%3A%2F%2Fyoutu.be%2FzLjf5P7Ef5k');
  assert.equal(YT.findURL('   ', 'Le Vent', 'Someone'), '');
});

test('lengths', () => {
  assert.equal(YT.duration(271), '4:31');
  assert.equal(YT.duration(59.6), '1:00');
  assert.equal(YT.duration(3725), '1:02:05');
  assert.equal(YT.duration(0), '');
  assert.equal(YT.duration(undefined), '');
});

test('the same video, whichever address', () => {
  assert.ok(YT.sameVideo('https://youtu.be/zLjf5P7Ef5k', 'https://www.youtube.com/watch?v=zLjf5P7Ef5k'));
  assert.ok(!YT.sameVideo('https://youtu.be/zLjf5P7Ef5k', 'https://youtu.be/AAAAAAAAAAA'));
  assert.ok(YT.sameVideo('https://example.com/a', ' https://example.com/a'));
  assert.ok(!YT.sameVideo('', ''));
  assert.ok(!YT.sameVideo('https://youtu.be/zLjf5P7Ef5k', ''));
});

test('a result\'s line: channel, length, what it is, in use', () => {
  const c = { url: 'https://www.youtube.com/watch?v=zLjf5P7Ef5k', channel: 'Buddy Guy', official: true, duration: 271, note: 'a live version' };
  assert.deepEqual(YT.metaParts(c, ''), ["Buddy Guy — the artist's channel", '4:31', 'a live version']);
  assert.deepEqual(YT.metaParts({ url: c.url, channel: 'x' }, 'https://youtu.be/zLjf5P7Ef5k'), ['x', 'in use']);
  assert.equal(YT.thumbOf('zLjf5P7Ef5k'), 'https://i.ytimg.com/vi/zLjf5P7Ef5k/mqdefault.jpg');
  assert.equal(YT.thumbOf(''), '');
});
