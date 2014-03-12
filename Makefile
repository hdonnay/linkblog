all: linkblog

linkblog: build/exe build/static.zip
	cat build/exe build/static.zip > linkblog
	zip -A linkblog
	chmod +x linkblog

build/static.zip: build static/* tmpl/*
	zip -r build/static.zip static tmpl

build/exe: *.go build
	go build -o build/exe

build:
	mkdir build

clean:
	rm linkblog
	rm -r build

.PHONY: clean
