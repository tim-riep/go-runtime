# Builds the packages with a user interface: TRFRONTEND into
# packages/TRFRONTEND/dist/TRFRONTEND and TRFINUI with its remote apps into
# packages/TRFINUI/dist/TRFINUI; the Go part needs no Node.
#
# The tests of the kernel against PostgreSQL (RT_TEST_POSTGRES_DSN) take
# close to ten minutes, the default timeout of go test; test allows more.

.PHONY: trfrontend trfinui test

trfrontend:
	cd packages/TRFRONTEND/web && npm ci && npm run build

trfinui:
	cd packages/TRFINUI/web && npm ci && npm run build

test:
	go test -timeout 30m ./...
