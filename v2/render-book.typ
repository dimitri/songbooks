// Ad-hoc book assembly for on-demand PDF export (setlists, songbooks, or
// any explicit ordered song list) — reuses lib/book.typ's book() unchanged.
// Not part of the normal `make` pipeline; driven by the app's PDF export.
//
//   typst compile --root . --input order-file=/.export-tmp-x/order.txt \
//                 --input songdir=/.export-tmp-x/songs \
//                 --input fits-file=/.export-tmp-x/fits.json \
//                 --input title="My Setlist" render-book.typ out.pdf
//
// Front matter (cover + title/event page): always a cover() with
// cover-title (falls back to title). Then EITHER an event-title-plate
// (event-title given, or date/venue given — a setlist tied to a real gig)
// OR a plain title-plate (band/songbook export, no single event). Passing
// one of date/venue without the other is fine (event-title-plate already
// omits whichever line is blank); passing neither is what selects the
// plain title-plate branch.
#import "lib/book.typ": book, cover, title-plate, event-title-plate

#let order-file   = sys.inputs.at("order-file")
#let songdir      = sys.inputs.at("songdir")
#let fits-file    = sys.inputs.at("fits-file", default: none)
#let target       = sys.inputs.at("target", default: "a4")
#let dark         = sys.inputs.at("theme", default: "light") == "dark"
#let title        = sys.inputs.at("title", default: "Setlist")
#let cover-title  = sys.inputs.at("cover-title", default: title)
#let event-title  = sys.inputs.at("event-title", default: "")
#let band-name    = sys.inputs.at("band-name", default: "")
#let event-date   = sys.inputs.at("event-date", default: "")
#let event-venue  = sys.inputs.at("event-venue", default: "")

#let fits = if fits-file != none { json(fits-file) } else { (:) }

#let has-event = event-title != "" or event-date != "" or event-venue != ""
#let front = (
  t => cover(cover-title, t: t),
  if has-event {
    t => event-title-plate(if event-title != "" { event-title } else { title },
                            band-name, event-date, event-venue, t: t)
  } else {
    t => title-plate(title, t: t)
  },
)

#show: body => book(order-file, songdir, target: target, dark: dark, fits: fits,
                     title: title, front: front)
