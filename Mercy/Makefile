SRC = Mercy.tex $(wildcard *.ly)
PDF = Mercy.pdf

IDXSCRIPT = /usr/local/texlive//2024/texmf-dist/doc/latex/songs/songidx/songidx.lua

all: pdf
	open $(PDF)

pdf: $(PDF) ;

$(PDF): $(SRC)
	lualatex --shell-escape $^

clean: aux-clean
	rm -f $(PDF)

aux-clean:
	rm -f *.{aux,log,sxc} idxfile.sxd idxfile.sbx
	rm -rf tmp-ly
