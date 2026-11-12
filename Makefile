.PHONY: all test vet clean package

all:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o sample-edit ./src

test:
	go test ./src/...

vet:
	GOOS=linux GOARCH=amd64 go vet ./src/...

# dist/sample-edit/ with the binary and hack.json, as Push Hack Catalog wants
package: all
	rm -rf dist && mkdir -p dist/sample-edit
	cp sample-edit hack.json dist/sample-edit/
	tar -czf sample-edit.tar.gz -C dist sample-edit/

clean:
	rm -rf sample-edit dist sample-edit.tar.gz
