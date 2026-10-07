BINARY := devmachine
VERSION ?= dev

.PHONY: accept accept-shell build surface settings widget-format test test-vps test-vps-sudo cover vps-up vps-down fmt lint docs run

accept:
	scripts/accept/run.sh

accept-shell:
	bash scripts/accept/common_test.sh

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) ./cmd/devmachine

# SURFACE.txt is committed, so a change to the command surface shows up in a
# diff rather than passing unnoticed.
surface:
	go run ./cmd/surface > SURFACE.txt

test:
	go test -race ./...

# The floor, not the target. It is set at a number the tree already clears, so
# it catches a regression instead of teaching everybody to ignore a red gate.
# Raise it when the number earns it.
COVERAGE_FLOOR := 80

cover:
	@go test -coverprofile=coverage.out ./internal/... >/dev/null
	@go tool cover -func=coverage.out | tail -1
	@go tool cover -func=coverage.out | tail -1 | awk '{gsub(/%/,"",$$3); \
		if ($$3+0 < $(COVERAGE_FLOOR)) { \
			printf "coverage %s%% is below the %d%% floor\n", $$3, $(COVERAGE_FLOOR); exit 1 }}'

# The integration tests skip without a machine to reach. This starts the
# throwaway VPS and points them at it.
test-vps: vps-up
	eval "$$(scripts/fake-vps.sh env)" && go test -race ./...

# The same suite with an admin login that is not root and reaches root through
# passwordless sudo. The suite assumed root until a real machine said otherwise.
test-vps-sudo:
	DEVMACHINE_FAKE_VPS_ADMIN=opsadmin scripts/fake-vps.sh up
	eval "$$(DEVMACHINE_FAKE_VPS_ADMIN=opsadmin scripts/fake-vps.sh env)" && go test -race ./...

vps-up:
	scripts/fake-vps.sh up

vps-down:
	scripts/fake-vps.sh down

fmt:
	gofmt -w .

lint:
	golangci-lint run

# The settings page is generated from the packages' own manifests, for the same
# reason SURFACE.txt is: a page kept by hand beside the thing it describes
# drifts. PACKAGES says where that repository is checked out.
PACKAGES ?= $(HOME)/dev/packages

settings:
	go run ./cmd/settings $(PACKAGES)/packages > docs/reference/settings.md

# The tables in the widget format page come from the engine contract, for the
# same reason as SURFACE.txt. A test fails when they are out of date.
widget-format:
	go run ./cmd/widgetformat

# Every link resolves, and every page is reachable from the index.
docs:
	scripts/check-docs.sh

run: build
	./$(BINARY) $(ARGS)
