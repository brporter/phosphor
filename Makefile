.PHONY: all build build-cli build-relay build-web dev-relay dev-web clean

all: build

build: build-web build-cli build-relay

build-cli:
	go build -ldflags '-s -w -X github.com/brporter/phosphor/internal/cli.DefaultRelayURL=wss://phosphor.betaporter.dev' -o bin/phosphor ./cmd/phosphor

build-relay:
	go build -o bin/relay ./cmd/relay

build-web:
	cd web && npm ci && npm run build

dev-relay:
	go run ./cmd/relay

dev-web:
	cd web && npm run dev

clean:
	rm -rf bin/ web/dist/ web/node_modules/
