PLUGIN := .claude-plugin/plugin.json
MARKETPLACE := .claude-plugin/marketplace.json

.PHONY: build test lint fmt install check version-check

build:
	go build -o .bin/agent-to-nvim .

test:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	go fmt ./...

install:
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
