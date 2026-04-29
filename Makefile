BINARY := main

.PHONY: linux darwin clean

# Cross-compile for DBOS Cloud (linux/amd64). Output: ./main
linux:
	GOOS=linux GOARCH=amd64 go build -o $(BINARY) .

# Build for local development on macOS.
darwin:
	GOOS=darwin go build -o $(BINARY) .

clean:
	rm -f $(BINARY)
