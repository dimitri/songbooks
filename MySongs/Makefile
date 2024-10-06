SRC = $(wildcard *.tex) $(wildcard */*.tex) $(wildcard */*.ly)

PDF_A4 = My-SongBook-A4.pdf
PDF_A6 = My-SongBook-A6.pdf
PDF_TABLET = My-SongBook-Tablet.pdf

PDFs = $(PDF_A4) $(PDF_A6) $(PDF_TABLET)  

all: pdfs ;

a4: $(PDF_A4)
	open $^	

a6: $(PDF_A6)
	open $^	

tablet: $(PDF_TABLET)
	open $^	

pdfs: $(PDFs) ;

clean: aux-clean
	rm -f $(PDFs)

aux-clean:
	rm -f *.{aux,log,sxc} idxfile.sxd idxfile.sbx
	rm -rf tmp-ly

%.pdf: %.tex $(SRC)
	lualatex --shell-escape $<

