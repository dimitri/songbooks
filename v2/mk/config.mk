# Shared configuration.

TYPST    ?= typst
LILYPOND ?= lilypond
PYTHON   ?= python3

# Which book, which page target, which theme.
BOOK   ?= Mercy
TARGET ?= a4
THEME  ?= light

# GNU make cannot express a path containing a space, so the book directory
# here is spaceless; SRCBOOK maps it back to the original LaTeX tree.
#
# "Other Lies" itself has a space, which isn't fixed by SRCBOOK being
# spaceless: mk/scores.mk's $(wildcard $(SRCDIR)/*/*.ly) silently splits on
# that space into two bogus patterns and returns nothing, and the $(shell
# grep -l ... $(ly_all)) right after it then runs with no file arguments --
# which reads stdin instead, and hangs forever in an interactive shell
# waiting for input nobody is going to type. ../OtherLies-src is a spaceless
# symlink to ../Other Lies (`ln -s "Other Lies" OtherLies-src`, run once from
# the repo root) that sidesteps this -- Make has no clean way to carry a
# space through its list machinery once every file under here inherits it.
SRCBOOK ?= $(if $(filter OtherLies,$(BOOK)),OtherLies-src,$(BOOK))
SRCDIR   = ../$(SRCBOOK)

BUILD   ?= build
OUT      = $(BUILD)/$(BOOK)/$(TARGET)-$(THEME)

SONGDIR  = $(BOOK)/songs
SCOREDIR = $(BOOK)/scores

RENDERER = render.typ $(wildcard lib/*.typ)

# Fonts live with the project so a CI build renders identically to a local one.
FONTDIR ?= fonts
TYPST_FLAGS ?= --root . $(if $(wildcard $(FONTDIR)/*),--font-path $(FONTDIR),)

ALL_TARGETS ?= a4 tablet13 tablet11
ALL_BOOKS   ?= Mercy OtherLies BandsCamp
