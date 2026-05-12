APP := dur
PKG := ./cmd/dur
DIST_DIR := dist
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.Version=$(VERSION)

PLATFORMS := darwin/amd64 linux/amd64

.PHONY: all test build dist clean release-tag

all: test build

test:
	go test ./...

build:
	go build -ldflags "$(LDFLAGS)" -o $(APP) $(PKG)

dist: clean test
	@mkdir -p $(DIST_DIR)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		bin="$(DIST_DIR)/$(APP)-$${os}-$${arch}"; \
		echo "building $$bin"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o "$$bin" $(PKG); \
		archive="$(DIST_DIR)/aidur-$(VERSION)-$${os}-$${arch}.tar.gz"; \
		tmpdir="$$(mktemp -d)"; \
		cp "$$bin" "$$tmpdir/$(APP)"; \
		cp README.md "$$tmpdir/README.md"; \
		tar -C "$$tmpdir" -czf "$$archive" $(APP) README.md; \
		rm -rf "$$tmpdir"; \
	done
	@cd $(DIST_DIR) && shasum -a 256 *.tar.gz > checksums.txt

clean:
	rm -rf $(DIST_DIR) $(APP)

release-tag:
	@test "$(origin VERSION)" = "command line" || (echo "VERSION is required, e.g. make release-tag VERSION=v0.1.0" && exit 1)
	git tag -a $(VERSION) -m "$(VERSION)"
	git push origin $(VERSION)
