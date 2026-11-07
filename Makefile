.PHONY: all test vet clean

all:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o sample-edit ./src

test:
	go test ./src/...

vet:
	GOOS=linux GOARCH=amd64 go vet ./src/...

clean:
	rm -f sample-edit
