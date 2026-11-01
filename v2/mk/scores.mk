# LilyPond fragments -> SVG.
#
# Only a handful of fragments carry real notation (a TabStaff riff or a staff);
# everything else is a chord grid and is drawn natively by the renderer. These
# are built once and committed, so the document build has no LilyPond
# dependency at all.
#
# tools/extract-score.py pulls out just the notation blocks and gives them
# their own paper block: the repo's header.ly pins line-width to 6cm, which
# wraps a riff into unreadable narrow systems, and the chord half would
# otherwise duplicate what the renderer already draws.

# Every .ly in the source tree, at both nesting depths (Other Lies adds an
# album level).
ly_all := $(wildcard $(SRCDIR)/*/*.ly) $(wildcard $(SRCDIR)/*/*/*.ly)
# ...restricted to those that actually contain notation.
ly_score := $(shell grep -l 'TabStaff\|\\relative\|\\clef' $(ly_all) 2>/dev/null)

# ../Mercy/bad-spell/intro.ly  ->  Mercy/scores/bad-spell-intro.svg
score-name = $(SCOREDIR)/$(notdir $(patsubst %/,%,$(dir $1)))-$(notdir $(basename $1)).svg

SCORES := $(foreach f,$(ly_score),$(call score-name,$(f)))

# One rule per fragment, because the source path cannot be recovered from the
# flattened target name by pattern matching alone.
define score_rule
$(call score-name,$1): $1 tools/extract-score.py
	@mkdir -p $$(@D)
	@echo "  lilypond $$(@F)"
	@$$(PYTHON) tools/extract-score.py "$$<" "$$@" $(SCORE_WIDTH_MM)
endef

SCORE_WIDTH_MM ?= 170

$(foreach f,$(ly_score),$(eval $(call score_rule,$(f))))

.PHONY: scores
# The SVGs are committed, so a build that has no LilyPond (CI, or a clone
# without it) can skip them entirely.
ifeq ($(SKIP_SCORES),1)
scores: ;
else
scores: $(SCORES) ;
endif
