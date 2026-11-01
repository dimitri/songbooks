#import "/lib/front.typ": front, title-plate

#let target = sys.inputs.at("target", default: "a4")
#let dark   = sys.inputs.at("theme", default: "light") == "dark"

#front(
  "/BandsCamp/order.txt", "/BandsCamp/songs",
  target: target, dark: dark,
  plates-before: (t => title-plate("Bands Camp", subtitle: "Set List", t: t),),
)
