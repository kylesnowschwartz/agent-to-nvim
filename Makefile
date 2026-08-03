SKILL_LINK := $(HOME)/.claude/skills/agent-to-nvim

.PHONY: build test lint fmt install install-skill uninstall-skill

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

# A personal skill is reachable as /agent-to-nvim; a plugin skill would be
# /agent-to-nvim:agent-to-nvim. Symlinked so edits in this checkout take effect.
install-skill:
	ln -sfn $(CURDIR)/skills/agent-to-nvim $(SKILL_LINK)
	@echo "linked $(SKILL_LINK) -> $(CURDIR)/skills/agent-to-nvim"

uninstall-skill:
	rm -f $(SKILL_LINK)
