# Builds the package TRFRONTEND with its user interface into
# packages/TRFRONTEND/dist/TRFRONTEND; the Go part needs no Node.

.PHONY: trfrontend test

trfrontend:
	cd packages/TRFRONTEND/web && npm ci && npm run build

test:
	go test ./...
