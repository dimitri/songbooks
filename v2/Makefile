# Strict one-page song sheets.
#
#   make                      build + verify Mercy on A4
#   make Mercy                same, by name -- also OtherLies, BandsCamp
#   make Mercy-book           build + assemble that book's songbook PDF
#   make BOOK=OtherLies       the general form the name-targets wrap
#   make TARGET=tablet13 THEME=dark
#   make all                  every book x every target
#   make scores               re-render the LilyPond notation fragments
#   make import BOOK=Mercy    re-import from the legacy LaTeX sources
#   make book                 assemble a songbook PDF
#
# Books are directories holding songs/*.song; see README.md.

include mk/config.mk
include mk/scores.mk
include mk/sheets.mk
include mk/books.mk

.DEFAULT_GOAL := check

.PHONY: all everything import clean distclean help

# Every book x every target, light theme, plus dark for the tablet.
everything:
	@for b in $(ALL_BOOKS); do \
	  for t in $(ALL_TARGETS); do \
	    $(MAKE) --no-print-directory sheets BOOK=$$b TARGET=$$t THEME=light || exit 1; \
	  done; \
	  $(MAKE) --no-print-directory sheets BOOK=$$b TARGET=tablet13 THEME=dark || exit 1; \
	done

all: everything ;

# One target per book, so you don't have to remember BOOK=Name -- `make
# Mercy` is `make BOOK=Mercy`, `make Mercy-book` also assembles its songbook
# PDF. Generated from ALL_BOOKS (mk/config.mk), so a new book picks these up
# automatically.
define BOOK_TARGETS
.PHONY: $1 $1-book
$1:
	@$$(MAKE) --no-print-directory check BOOK=$1
$1-book:
	@$$(MAKE) --no-print-directory check BOOK=$1
	@$$(MAKE) --no-print-directory book BOOK=$1
endef
$(foreach b,$(ALL_BOOKS),$(eval $(call BOOK_TARGETS,$(b))))

# One-shot import from the original LaTeX + LilyPond sources. Not part of a
# normal build: the .song files are the source of truth once imported, and
# re-running this overwrites hand edits.
# Hand corrections (tools/corrections.py) run AFTER conversion, every time,
# so a re-import can never silently clobber a fix applied on top of the
# literal source transcription again -- that happened once (the Colors chord
# sequence) before this step existed.
import:
	@$(PYTHON) tools/convert.py "$(SRCDIR)" "$(SONGDIR)"
	@$(PYTHON) tools/corrections.py "$(BOOK)"

clean:
	rm -rf $(BUILD)

# Also drops the rendered score fragments, which need LilyPond to rebuild.
distclean: clean
	rm -f $(SCOREDIR)/*.svg

help:
	@awk '/^# Books are/{exit} NR>1{sub(/^# ?/,""); print}' Makefile
