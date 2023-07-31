
gofiles := $(shell find . -name '*.go' -type f -not -path "./vendor/*")
buildtime := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
commit := $(shell bash ./bin/gitcommit.sh)
flags := -X main.BuildVersion=dev -X main.BuildArch=dev -X main.BuildCommit=${commit} -X main.BuildTimestamp=${buildtime} -X main.BuildOs=dev

all: build test

bin/tako: $(gofiles) Makefile go.mod go.sum
	mkdir -p bin
	go build -ldflags "${flags}" -o ./bin/tako

clean:
	rm -f bin/tako

watch: Makefile
	find . -name "*.go" -o -name "Makefile" | entr -c make

test: bin/tako
	./bin/tako symbols .
	./bin/tako tree main.go -d 4
	./bin/tako symbol main.go PrintSymbolsMatching
	./bin/tako symbol main.go ParsedDocument

build: bin/tako

licenses:
	go-licenses report ./... 2>/dev/null | awk -F"," '{printf "|[%s](https://%s)|[%s](%s)|\n",$$1,$$1,$$3,$$2}'


.PHONY: all clean watch test build licenses

