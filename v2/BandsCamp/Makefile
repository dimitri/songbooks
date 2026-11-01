# Thin wrapper so `make -C Mercy` (or `cd Mercy && make`) works directly,
# forwarding every target to the root Makefile with BOOK set from this
# directory's own name. TARGET=/THEME=/etc. passed on the command line
# propagate to the sub-make automatically (standard $(MAKE) behaviour).
BOOK := $(notdir $(patsubst %/,%,$(CURDIR)))

.DEFAULT_GOAL := check
.PHONY: check book sheets scores import clean distclean everything all

check book sheets scores import clean distclean everything all:
	@$(MAKE) -C .. BOOK=$(BOOK) $@
