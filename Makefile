all: linkblog

linkblog: build/exe build/static.zip
	cat build/exe build/static.zip > linkblog
	zip -A linkblog
	chmod +x linkblog

build/static.zip: build static/* tmpl/* static/style.css
	zip -r build/static.zip static tmpl

build/exe: *.go build
	go build -o build/exe

build:
	mkdir build

static/style.css: static/style.scss
	sass static/style.scss static/style.css
	@rm -r .sass-cache

clean:
	rm linkblog
	rm -r build

.PHONY: clean
