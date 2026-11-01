// Per-song sheet entry point.
//   typst compile --input song=Mercy/songs/banshee.song \
//                 --input target=a4 --input theme=light render.typ out.pdf
#import "lib/song.typ": parse-song
#import "lib/sheet.typ": sheet

#let path   = sys.inputs.at("song")
#let target = sys.inputs.at("target", default: "a4")
#let dark   = sys.inputs.at("theme", default: "light") == "dark"
#let ceil   = sys.inputs.at("ceiling", default: "")

#show: body => sheet(
  parse-song(path), target: target, dark: dark,
  ceiling: if ceil == "" { none } else { float(ceil) * 1pt },
)
