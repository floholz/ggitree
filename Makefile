PREFIX ?= $(HOME)/.local
# The tag on this commit, else the nearest tag plus commits (v1.1.0-3-gabc1234), else "dev".
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/floholz/ggitree/internal/server.Version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.PHONY: build web watch test run screenshot install install-t3 install-service uninstall uninstall-t3 dist clean

build: web
	go build -trimpath -ldflags "$(LDFLAGS)" -o ggitree .

# Release binaries for every platform, plus their checksums, in dist/.
dist: web
	mkdir -p dist
	for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o dist/ggitree-$(VERSION)-$$os-$$arch$$ext . || exit 1; \
	done
	cd dist && sha256sum ggitree-$(VERSION)-* > SHA256SUMS-$(VERSION)

web:
	go generate ./web

# Rebuild the page on every change; run "go run . serve ." next to it.
watch:
	cd web && go run ../tools/build -watch

test:
	go test ./...

run: build
	./ggitree serve .

# Rebuilds docs/screenshot.png from a made-up demo repository.
screenshot: build
	tools/demo/screenshot.sh ./ggitree

install: build
	install -Dm755 ggitree $(PREFIX)/bin/ggitree
	$(PREFIX)/bin/ggitree t3-install

# Adds the action to T3 Code for every project. Safe to rerun; the first run
# backs up T3 Code's settings to ~/.t3/userdata/ggitree-backup.
install-t3: build
	./ggitree t3-install

# Starts ggitree at login, so the page is there the moment T3 Code opens it.
install-service: install
	install -Dm644 contrib/ggitree.service $(HOME)/.config/systemd/user/ggitree.service
	systemctl --user daemon-reload
	systemctl --user enable ggitree.service
	systemctl --user restart ggitree.service

uninstall: uninstall-t3
	-systemctl --user disable --now ggitree.service
	rm -f $(HOME)/.config/systemd/user/ggitree.service $(PREFIX)/bin/ggitree

uninstall-t3:
	-$(PREFIX)/bin/ggitree t3-uninstall || ./ggitree t3-uninstall

clean:
	rm -rf ggitree web/dist
