// Ad-hoc book assembly for on-demand PDF export (setlists, or any
// explicit ordered song list) — reuses lib/book.typ's book() unchanged.
// Not part of the normal `make` pipeline; driven by the app's PDF export.
//
//   typst compile --root . --input order-file=/.export-tmp-x/order.txt \
//                 --input songdir=/.export-tmp-x/songs \
//                 --input fits-file=/.export-tmp-x/fits.json \
//                 --input title="My Setlist" render-book.typ out.pdf
#import "lib/book.typ": book

#let order-file = sys.inputs.at("order-file")
#let songdir    = sys.inputs.at("songdir")
#let fits-file  = sys.inputs.at("fits-file", default: none)
#let target     = sys.inputs.at("target", default: "a4")
#let dark       = sys.inputs.at("theme", default: "light") == "dark"
#let title      = sys.inputs.at("title", default: "Setlist")

#let fits = if fits-file != none { json(fits-file) } else { (:) }

#show: body => book(order-file, songdir, target: target, dark: dark, fits: fits, title: title)
