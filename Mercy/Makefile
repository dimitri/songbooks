SRC = $(wildcard *.tex) $(wildcard */*.ly)

PDF_A4 = Mercy-SongBook-A4.pdf
PDF_PHONE = Mercy-SongBook-Phone.pdf
PDF_TABLET = Mercy-SongBook-Tablet.pdf

PDFs = $(PDF_A4) $(PDF_PHONE) $(PDF_TABLET)  

all: pdfs ;

a4: $(PDF_A4)
	open $^	

phone: $(PDF_PHONE)
	open $^	

tablet: $(PDF_TABLET)
	open $^	

pdfs: $(PDFs) ;

clean: aux-clean
	rm -f $(PDFs)

aux-clean:
	rm -f *.{aux,log,sxc} idxfile.sxd idxfile.sbx
	rm -rf tmp-ly

%.pdf: %.tex
	lualatex --shell-escape $^

