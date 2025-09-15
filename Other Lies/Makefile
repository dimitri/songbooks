SRC  = $(wildcard *.tex) $(wildcard */*.tex) $(wildcard */*/*.tex)
SRC += $(wildcard */*/*.ly) $(wildcard *.ly)

PDF_A4 = OtherLies-SongBook-A4.pdf
PDF_L4 = OtherLies-SongBook-L4.pdf

PDFs = $(PDF_A4) $(PDF_L4)

IDXSCRIPT = /usr/local/texlive//2024/texmf-dist/doc/latex/songs/songidx/songidx.lua

all: pdfs ;

a4: $(PDF_A4)
	open $^

l4: $(PDF_L4)
	open $^

pdfs: $(PDFs) ;

clean: aux-clean
	rm -f $(PDF)

setlist: OtherLies-2025.pdf
	open $^

vasarely: OtherLies-Vasarely-2024-09-27.pdf
	open $^

aux-clean:
	rm -f *.{aux,log,sxc} idxfile.sxd idxfile.sbx
	rm -rf tmp-ly

%.pdf: %.tex $(SRC)
	lualatex --shell-escape $<
	# texlua $(IDXSCRIPT) idxfile.sxd idxfile.sbx
	# lualatex --shell-escape $<

