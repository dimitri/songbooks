# The lesson page, next to the others

What a student gets on a lesson page at the platforms guitar teachers use,
read from their help pages and reviews in November 2026, and what our
lesson page (a song's page opened on its score, docs/courses.md) does
with it. Several help pages refused automated reading (Guitar Tricks,
Fender Play, Modern Metal Academy, a Palmisano course directory); what's
below is what could be read.

## What they do

- **TrueFire, Tab Sync** ([player][tf-sync], [progress][tf-progress],
  [formats][tf-formats]). Notation and tab in the browser, lit as it
  plays; click a note to go there; drag across the notation to loop,
  snapping to notes or barlines; slow down to 25%, the pitch kept;
  transpose, tab included; a fretboard lighting the notes played; zoom,
  the notation re-wrapping to the screen. Each lesson Incomplete, In
  progress or Complete, and the course's lesson table filtered by it.
  Courses come with jam tracks, tab, notation and Guitar Pro files to
  download; apps for iOS, Android, Apple TV and the desktop (offline).
- **Pickup Music** ([interactive notation][pickup]). The video and the
  notation synced, a divider between them to share the screen, the video
  movable to the side, hidden or mirrored for left-handers; click to move
  the playhead; drag over bars to loop, with a focus mode showing only
  those; speed in percent; metronome, or a count-in with the video; Space,
  ←/→ by note, ⌘←/→ by bar, Enter to the start; tab, staff or both; a
  fretboard; backing tracks by part left out ("No guitar", "No bass");
  full screen on an iPad, not on an iPhone.
- **Soundslice** ([slowdown and looping][soundslice]). Slowdown that keeps
  the sound smooth while the speed changes, gapless loops (each pass the
  same), a slice with an MP3 and a video, the teacher picking which.
- **Guitargate, Michael Palmisano** ([review][guitargate]). The courses as
  cards; a course page with its goals, a progress tracker and *Begin
  course*; a lesson's video (several camera angles) with its jam track,
  its PDF and a discussion under it; students post videos for the
  teacher's feedback; a weekly live class.
- **Rhett Shull** ([his Teachable school][rhett]), **Brett Papa**
  ([brettpapa.com][papa]). Courses sold one by one (and a membership at
  Papa's), lessons of videos with PDFs and backing tracks: the course
  platform's own player (Teachable: a lecture list, *Complete and
  continue*), no tab synced to anything.

## What we have that they don't

The lesson opens on the song's own Guitar Pro score, playing the file's
own recording under the cursor, with speed, loop, click and count-in;
the same song in the band's tools (song map, chart, mixer, set lists);
the teacher's files and video beside it. Teachable-style platforms (Rhett
Shull, Brett Papa) have no tab at all; TrueFire and Pickup build their
synced notation per lesson, in house.

## What they do that we didn't, and what we did

Done now (the song's page, play.html, gp-view.js):

- **The course around the lesson**: the lessons before and after, how many
  are done (a meter), and once done, the next lesson as the way on
  (Teachable's *Complete and continue*, TrueFire's progress).
- **The video and the score at once**: the open video docks in a corner
  once scrolled past, still playing, and closes with its × (Pickup's
  video beside the notation).
- **Slower**: 25% and 40% (TrueFire goes down to 25%), and 110%.
- **Saying what the score does**: click to start there, drag across bars
  to loop them (both had it, unsaid).

Later, by what they'd bring:

1. **The video synced with the score**: the lesson's video as the
   recording the cursor follows (alphaTab plays external media with sync
   points, so a YouTube video can drive it); its sync points set once by
   the teacher. Pickup and TrueFire's core. A big build.
2. **Backing by part**: the file's tracks muted or soloed ("No guitar"),
   in the Score view.
3. **A fretboard** lighting the notes played (TrueFire, Pickup).
4. **Lessons In progress** (opened, not done) and the course's list
   filtered by it (TrueFire).
5. **A loop's own view**: only the looped bars on screen (Pickup's focus
   mode), and a speed trainer in the Score view (the practice player has
   one).
6. **Feedback**: a student's video or question to the teacher, under the
   lesson (Guitargate). Needs the teacher's time more than code.
7. **Left-handed**: the fretboard and video mirrored (Pickup).

[tf-sync]: https://intercom.help/truefire/en/articles/10928493-using-the-tab-sync-features
[tf-progress]: https://intercom.help/truefire/en/articles/10771278-how-to-track-your-progress-on-courses
[tf-formats]: https://intercom.help/truefire/en/articles/11653751-what-formats-are-available-for-truefire-courses
[pickup]: https://help.pickupmusic.com/en/articles/10591947-how-to-use-the-interactive-notation
[soundslice]: https://www.soundslice.com/blog/199/introducing-enhanced-slowdown-and-perfect-looping/
[guitargate]: https://www.guitarchalk.com/guitar-gate-review/
[rhett]: https://rhett-shull-s-music-courses.teachable.com/courses
[papa]: https://brettpapa.com/
