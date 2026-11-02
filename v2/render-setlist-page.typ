// A standalone single-page setlist export — just the running order
// (#, Title, Key, Tempo), grouped by set, no song pages. For a music
// stand or a quick printout, not the full songbook.
//
//   typst compile --root . --input order-file=/.export-tmp-x/order.txt \
//                 --input songdir=/.export-tmp-x/songs \
//                 --input title="My Setlist" --input subtitle="Nov 14 · The Loft" \
//                 render-setlist-page.typ out.pdf
#import "lib/book.typ": render-setlist-page

#let order-file = sys.inputs.at("order-file")
#let songdir    = sys.inputs.at("songdir")
#let target     = sys.inputs.at("target", default: "a4")
#let dark       = sys.inputs.at("theme", default: "light") == "dark"
#let title      = sys.inputs.at("title", default: "Setlist")
#let subtitle-in = sys.inputs.at("subtitle", default: "")
#let subtitle   = if subtitle-in != "" { subtitle-in } else { none }

#render-setlist-page(order-file, songdir, target: target, dark: dark,
                      title: title, subtitle: subtitle)
