SRC = OtherLies-Songbook.tex $(wildcard *.ly)
PDF = OtherLies-Songbook.pdf

IDXSCRIPT = /usr/local/texlive//2024/texmf-dist/doc/latex/songs/songidx/songidx.lua

all: pdf
	open $(PDF)

pdf: $(PDF) ;

$(PDF): $(SRC)
	lualatex --shell-escape $^
	texlua $(IDXSCRIPT) idxfile.sxd idxfile.sbx
	lualatex --shell-escape $^

clean: aux-clean
	rm -f $(PDF)

aux-clean:
	rm -f *.{aux,log,sxc} idxfile.sxd idxfile.sbx
	rm -rf tmp-ly
