BINARY := agent-to-nvim
PLUGIN := .claude-plugin/plugin.json
MARKETPLACE := .claude-plugin/marketplace.json
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
RELEASE_DIR ?= dist
DEV_MARKETPLACE := .dev-marketplace

.PHONY: build test lint fmt install install-go check version-check release-check release-build release

# The launcher in bin/ runs the binary named for this platform, so a checkout
# builds under that name and the hook command works against the working tree.
build:
	go build -ldflags "-X main.version=$(VERSION)" \
		-o bin/$(BINARY)_$(shell go env GOOS)_$(shell go env GOARCH) .

test:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	go fmt ./...

# A marketplace entry can only name a source below its own marketplace root, so
# a development install gets a marketplace of its own holding a copy of the
# plugin tree with the binary this checkout built.
install: build
	rm -rf $(DEV_MARKETPLACE)
	mkdir -p $(DEV_MARKETPLACE)/.claude-plugin
	cp scripts/dev-marketplace.json $(DEV_MARKETPLACE)/.claude-plugin/marketplace.json
	./scripts/assemble-plugin.sh $(DEV_MARKETPLACE)/plugin
	cp bin/$(BINARY)_$(shell go env GOOS)_$(shell go env GOARCH) $(DEV_MARKETPLACE)/plugin/bin/
	claude plugin marketplace add $(CURDIR)/$(DEV_MARKETPLACE)
	claude plugin marketplace update agent-to-nvim-dev
	claude plugin install $(BINARY)@agent-to-nvim-dev
	@echo "installed from $(DEV_MARKETPLACE); restart Claude Code so it loads the hook"

# Putting the binary on PATH, for use outside the Claude Code plugin.
install-go:
	go install .

check: test lint version-check

# The plugin manifest keys the install cache and the marketplace entry is what a
# reader browsing the catalogue sees, so a bump applied to one and not the other
# looks like it worked from either side alone.
version-check:
	@plugin=$$(jq -r '.version' $(PLUGIN)); \
	catalogue=$$(jq -r '.plugins[] | select(.name == "agent-to-nvim") | .version' $(MARKETPLACE)); \
	if [ "$$plugin" != "$$catalogue" ]; then \
		echo "version mismatch: $(PLUGIN) says $$plugin, $(MARKETPLACE) says $$catalogue" >&2; \
		echo "bump whichever is behind so an install and the catalogue agree" >&2; \
		exit 1; \
	fi; \
	echo "version $$plugin in both manifests"

# A release is named by its tag and installed by the manifest version, so a
# release whose names disagree installs bytes nobody can identify. A prerelease
# suffix names the same version, so it is dropped before the comparison.
release-check: version-check
	@tag='$(TAG)'; \
	if [ -z "$$tag" ]; then \
		echo 'release-check: name the tag, for example make release-check TAG=v0.9.0' >&2; \
		exit 2; \
	fi; \
	case "$$tag" in \
		v?*) ;; \
		*) \
			echo "release-check: tag $$tag must be v<version> or v<version>-<suffix>, for example v0.9.0 or v0.9.0-rc1" >&2; \
			exit 1 ;; \
	esac; \
	tag_version=$${tag#v}; \
	tag_version=$${tag_version%%-*}; \
	manifest_version=$$(jq -r '.version' $(PLUGIN)); \
	if [ "$$tag_version" != "$$manifest_version" ]; then \
		echo "release-check: tag $$tag does not match the version $$manifest_version in $(PLUGIN) and $(MARKETPLACE); change one of them so they agree" >&2; \
		exit 1; \
	fi; \
	echo "release-check: tag $$tag matches the version $$manifest_version in both manifests"

release-build: release-check
	./scripts/build-release.sh '$(TAG)' '$(RELEASE_DIR)'

# A release is a tag on main: check it against the manifests, tag the current
# commit, push the tag. GitHub Actions builds the binaries from the tag and
# pushes the dist branch.
release: release-check
	@if [ -n "$$(git status --porcelain)" ]; then echo 'release: the working tree has uncommitted changes' >&2; exit 1; fi
	@if [ "$$(git rev-parse --abbrev-ref HEAD)" != "main" ]; then echo 'release: tag from main' >&2; exit 1; fi
	git tag -a '$(TAG)' -m 'agent-to-nvim $(TAG)'
	git push origin '$(TAG)'
	@echo "release: $(TAG) pushed; GitHub Actions publishes it to the dist branch"
