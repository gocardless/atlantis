.PHONY: all clean native amd64

all: native amd64

native: bin/tfsplit

amd64: bin/tfsplit-amd64

# Native binary
bin/tfsplit: *.go go.mod
	go build -o bin/tfsplit

# AMD64 binary for running in terraform container
bin/tfsplit-amd64: *.go go.mod
	GOOS=linux GOARCH=amd64 go build -o bin/tfsplit-amd64

clean:
	rm -f bin/*
