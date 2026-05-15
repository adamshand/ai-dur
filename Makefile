APP := dur
PKG := ./cmd/dur
DIST_DIR := dist
RELEASE_NOTES := $(DIST_DIR)/release-notes.md
STAMP := $(shell date -u +%Y%m%d-%H%M)
VERSION ?= dev-$(STAMP)
RELEASE_VERSION ?= $(STAMP)
LDFLAGS := -s -w -X main.Version=$(VERSION)

PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64

.PHONY: all test build dist clean release release-check release-notes release-tag

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

release-check:
	@command -v gh >/dev/null || (echo "gh CLI is required for release" && exit 1)
	@gh auth status >/dev/null || (echo "gh CLI is not authenticated" && exit 1)
	@git rev-parse --is-inside-work-tree >/dev/null
	@! git rev-parse --verify --quiet "refs/tags/$(VERSION)" >/dev/null || (echo "tag $(VERSION) already exists locally" && exit 1)
	@! git ls-remote --exit-code --tags origin "refs/tags/$(VERSION)" >/dev/null 2>&1 || (echo "tag $(VERSION) already exists on origin" && exit 1)
	@! gh release view "$(VERSION)" >/dev/null 2>&1 || (echo "GitHub release $(VERSION) already exists" && exit 1)

release-notes:
	@mkdir -p $(DIST_DIR)
	@git fetch --quiet --tags origin
	@last_release="$$(gh release list --limit 1 --json tagName --jq '.[0].tagName // ""')"; \
	if [ -n "$$last_release" ]; then \
		range="$$last_release..HEAD"; \
		scope="Changes since $$last_release"; \
	else \
		range="HEAD"; \
		scope="Changes"; \
	fi; \
	{ \
		echo "dur $(VERSION)"; \
		echo; \
		echo "$$scope:"; \
		echo; \
		commits="$$(git log --pretty=format:'- %s (%h)' $$range)"; \
		if [ -n "$$commits" ]; then \
			printf '%s\n' "$$commits"; \
		else \
			echo "- No commits since $$last_release."; \
		fi; \
	} > $(RELEASE_NOTES)

release: VERSION := $(RELEASE_VERSION)
release: release-check
	$(MAKE) dist VERSION=$(VERSION)
	$(MAKE) release-notes VERSION=$(VERSION)
	git tag -a $(VERSION) -m "$(VERSION)"
	git push origin $(VERSION)
	gh release create $(VERSION) $(DIST_DIR)/*.tar.gz $(DIST_DIR)/checksums.txt --title "$(VERSION)" --notes-file "$(RELEASE_NOTES)"

clean:
	rm -rf $(DIST_DIR) $(APP)

release-tag:
	@test "$(origin VERSION)" = "command line" || (echo "VERSION is required, e.g. make release-tag VERSION=20260514-1530" && exit 1)
	git tag -a $(VERSION) -m "$(VERSION)"
	git push origin $(VERSION)
