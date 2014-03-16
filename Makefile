all: linkblog

linkblog: build build/exe build/static.zip
	cat build/exe build/static.zip > linkblog
	zip -A linkblog
	chmod +x linkblog

build/static.zip: static/* tmpl/*
	zip -r build/static.zip static tmpl

build/exe: *.go
	go get -d .
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
