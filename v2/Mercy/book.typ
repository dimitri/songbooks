#import "/lib/book.typ": book, title-plate

#let target = sys.inputs.at("target", default: "a4")
#let dark   = sys.inputs.at("theme", default: "light") == "dark"
#let fitsf  = sys.inputs.at("fits", default: "")
#let fits   = if fitsf == "" { (:) } else { json(fitsf) }
#book(
  "/Mercy/order.txt", "/Mercy/songs",
  target: target, dark: dark, fits: fits, title: "Contents",
  front: (t => title-plate("Mercy", subtitle: "Song Book", t: t),),
)
