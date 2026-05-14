APP := dur
PKG := ./cmd/dur
DIST_DIR := dist
STAMP := $(shell date -u +%Y%m%d-%H%M)
VERSION ?= dev-$(STAMP)
RELEASE_VERSION ?= $(STAMP)
LDFLAGS := -s -w -X main.Version=$(VERSION)

PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64

.PHONY: all test build dist clean release release-tag

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
		echo "building $$bin version $(VERSION)"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o "$$bin" $(PKG); \
		archive="$(DIST_DIR)/$(APP)-$(VERSION)-$${os}-$${arch}.tar.gz"; \
		tmpdir="$$(mktemp -d)"; \
		cp "$$bin" "$$tmpdir/$(APP)"; \
		cp README.md "$$tmpdir/README.md"; \
		tar -C "$$tmpdir" -czf "$$archive" $(APP) README.md; \
		rm -rf "$$tmpdir"; \
	done
	@cd $(DIST_DIR) && shasum -a 256 *.tar.gz > checksums.txt

release: VERSION := $(RELEASE_VERSION)
release: dist
	@command -v gh >/dev/null || (echo "gh CLI is required for release" && exit 1)
	git tag -a $(VERSION) -m "$(VERSION)"
	git push origin $(VERSION)
	gh release create $(VERSION) $(DIST_DIR)/*.tar.gz $(DIST_DIR)/checksums.txt --title "$(VERSION)" --notes "dur $(VERSION)"

clean:
	rm -rf $(DIST_DIR) $(APP)

release-tag:
	@test "$(origin VERSION)" = "command line" || (echo "VERSION is required, e.g. make release-tag VERSION=20260514-1530" && exit 1)
	git tag -a $(VERSION) -m "$(VERSION)"
	git push origin $(VERSION)
