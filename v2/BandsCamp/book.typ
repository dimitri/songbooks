#import "/lib/book.typ": book, title-plate, plates

#let target = sys.inputs.at("target", default: "a4")
#let dark   = sys.inputs.at("theme", default: "light") == "dark"
#let fitsf  = sys.inputs.at("fits", default: "")
#let fits   = if fitsf == "" { (:) } else { json(fitsf) }

#book(
  "/BandsCamp/order.txt", "/BandsCamp/songs",
  target: target, dark: dark, fits: fits, title: "Set List",
  front: (
    t => title-plate("Bands Camp", subtitle: "Set List", logo: "/BandsCamp/assets/bands-camp-logo.png", t: t),
  ),
)
