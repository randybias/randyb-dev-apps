APP ?= portwarden
BIN_DIR := bin

.PHONY: all build test fmt vet tidy clean

all: fmt vet test build

build:
	go build -o $(BIN_DIR)/$(APP) ./$(APP)

test:
	go test ./$(APP)/... -count=1

fmt:
	gofmt -w ./$(APP)

vet:
	go vet ./$(APP)/...

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR)
