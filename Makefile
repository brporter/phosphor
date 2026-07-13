.PHONY: wasm relay cli web test

# Build the browser SSH client to WebAssembly and stage it (plus the Go
# wasm_exec.js runtime) into web/public so Vite serves them.
wasm:
	GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o web/public/phosphor-ssh.wasm ./cmd/webssh
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" web/public/wasm_exec.js

relay:
	go build -o bin/relay ./cmd/relay

cli:
	go build -o bin/phosphor ./cmd/phosphor

web: wasm
	cd web && npm ci && npm run build

test:
	go test ./... -count=1
