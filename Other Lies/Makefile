SRC  = $(wildcard *.tex) $(wildcard */*.tex) $(wildcard */*/*.tex)
SRC += $(wildcard */*/*.ly) $(wildcard *.ly)

PDF_A4 = OtherLies-SongBook-A4.pdf
PDF_PHONE = OtherLies-SongBook-Phone.pdf
PDF_TABLET = OtherLies-SongBook-Tablet.pdf

PDFs = $(PDF_A4) $(PDF_PHONE) $(PDF_TABLET)  

IDXSCRIPT = /usr/local/texlive//2024/texmf-dist/doc/latex/songs/songidx/songidx.lua

all: pdfs
	open $(PDF)

a4: $(PDF_A4)
	open $^

pdfs: $(PDFs) ;

clean: aux-clean
	rm -f $(PDF)

aux-clean:
	rm -f *.{aux,log,sxc} idxfile.sxd idxfile.sbx
	rm -rf tmp-ly

%.pdf: %.tex $(SRC)
	lualatex --shell-escape $<
	# texlua $(IDXSCRIPT) idxfile.sxd idxfile.sbx
	# lualatex --shell-escape $<

