#import "/lib/book.typ": book, title-plate

#let target = sys.inputs.at("target", default: "a4")
#let dark   = sys.inputs.at("theme", default: "light") == "dark"
#let fitsf  = sys.inputs.at("fits", default: "")
#let fits   = if fitsf == "" { (:) } else { json(fitsf) }

// Each album's own cover introduces its own run of songs, matching the real
// album grouping in order.txt (built from each album's own songs.tex), rather
// than a single combined gallery page up front.
// Single line: see the note in Mercy/book.typ about multi-line chains at the
// top level of a .typ file not continuing the way a `{ }` code block would.
#import "/lib/book.typ": album-page
#let art(img) = (title, year, t, entries) => album-page(title, "/OtherLies/assets/" + img, entries, year: year, t: t)

#book(
  "/OtherLies/order.txt", "/OtherLies/songs",
  target: target, dark: dark, fits: fits, title: "Contents",
  front: (t => title-plate("Other Lies", subtitle: "Song Book",
                           logo: "/OtherLies/assets/Other-Lies-Logo.png", t: t),),
  dividers: (
    "Beyond Fur": art("beyond-fur.jpg"),
    "Surfacing":  art("surfacing.jpg"),
    "Ultrablanc": art("ultrablanc.png"),
  ),
)
