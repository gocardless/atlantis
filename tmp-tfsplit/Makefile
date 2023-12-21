.PHONY: all clean native amd64

all: native amd64 arm64

native: bin/tfsplit

amd64: bin/tfsplit-amd64

arm64: bin/tfsplit-arm64

# Native binary
bin/tfsplit: *.go go.mod
	go build -o bin/tfsplit

# AMD64 binary for running in terraform container
bin/tfsplit-amd64: *.go go.mod
	GOOS=linux GOARCH=amd64 go build -o bin/tfsplit-amd64

# ARM64 linux binary for running in terraform container (ARM64 architecture)
bin/tfsplit-arm64: *.go go.mod
	GOOS=linux GOARCH=arm64 go build -o bin/tfsplit-arm64

clean:
	rm -f bin/*
