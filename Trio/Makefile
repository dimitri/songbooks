SRC = $(wildcard *.tex) $(wildcard */*.tex) $(wildcard */*.ly)

PDF_A4 = Trio-A4.pdf

PDFs = $(PDF_A4)

all: pdfs ;

a4: $(PDF_A4)
	open $^	

pdfs: $(PDFs) ;

clean: aux-clean
	rm -f $(PDFs)

aux-clean:
	rm -f *.{aux,log,sxc} idxfile.sxd idxfile.sbx
	rm -rf tmp-ly

%.pdf: %.tex $(SRC)
	lualatex --shell-escape $<

