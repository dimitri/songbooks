# Assembled songbook = front matter + the per-song sheets, concatenated.
#
# The sheets are NOT re-rendered into the book. Each is already built and
# verified as exactly one page, so concatenating them is both far faster
# (re-running every fit bisection inside one document took minutes) and
# guarantees the book contains precisely the pages that passed the check.
#
# Running order lives in <Book>/order.txt, one slug per line -- a single source
# of truth shared by the contents table and this concatenation, replacing the
# old practice of commenting out \subimport lines.

BOOKSRC := $(wildcard $(BOOK)/book.typ)
ORDER    := $(BOOK)/order.txt
BOOKPDF   = $(BUILD)/$(BOOK)-$(TARGET)-$(THEME).pdf

.PHONY: book
ifeq ($(BOOKSRC),)
book:
	@echo "no $(BOOK)/book.typ -- nothing to assemble"
else
book: $(BOOKPDF) ;

# Depends on the sheets only for their .fit sidecars: the book renders each
# song itself (so contents links have destinations in the same file) but at the
# size the per-song build already settled on, not by searching again.
$(BOOKPDF): $(BOOKSRC) $(ORDER) $(SONGS) $(wildcard lib/*.typ) $(FITS)
	@mkdir -p $(@D)
	@echo "  book $(@F)"
	@$(TYPST) compile $(TYPST_FLAGS) $< $@ \
	   --input target=$(TARGET) --input theme=$(THEME) \
	   --input fits=/$(FITS) --diagnostic-format short
endif
