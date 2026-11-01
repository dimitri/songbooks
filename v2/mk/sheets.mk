# One PDF per song, per (target, theme).
#
# Dependency tracking is real: typst --deps-format make emits the actual input
# list per sheet, so editing one song rebuilds that song (~50ms) rather than
# relinking the whole book.

SONGS := $(wildcard $(SONGDIR)/*.song)
PDFS  := $(patsubst $(SONGDIR)/%.song,$(OUT)/%.pdf,$(SONGS))

$(OUT)/%.pdf: $(SONGDIR)/%.song $(RENDERER) | scores
	@TYPST_FLAGS="$(TYPST_FLAGS)" ./tools/compile-sheet.sh "$<" "$@" "$(TARGET)" "$(THEME)"

-include $(wildcard $(OUT)/*.d)

.PHONY: sheets check
sheets: $(PDFS) ;

# The one-page requirement is asserted at build time, not eyeballed. A sheet
# that cannot reach one page above the legibility floor is reported as an
# editorial problem with the song rather than silently shrunk.
check: sheets
	@./tools/check.sh "$(BOOK)" "$(TARGET)" "$(THEME)" "$(OUT)"

# The fit chosen for each sheet, collected for the book build so it can render
# at the known size instead of searching for it again.
FITS = $(OUT)/fits.json
$(FITS): $(PDFS)
	@python3 -c "import json,sys,glob,os; \
	  d={os.path.basename(f)[:-4]: json.loads(open(f).read().split(' ',1)[1]) \
	     for f in glob.glob('$(OUT)/*.fit')}; \
	  open('$@','w').write(json.dumps(d))"
