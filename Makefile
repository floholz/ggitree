PREFIX ?= $(HOME)/.local

.PHONY: build web watch test run install install-service uninstall clean

build: web
	go build -o ggitree .

web:
	go generate ./web

# Rebuild the page on every change; run "go run . serve ." next to it.
watch:
	cd web && go run ../tools/build -watch

test:
	go test ./...

run: build
	./ggitree serve .

install: build
	install -Dm755 ggitree $(PREFIX)/bin/ggitree

# Starts ggitree at login, so the page is there the moment T3 Code opens it.
install-service: install
	install -Dm644 contrib/ggitree.service $(HOME)/.config/systemd/user/ggitree.service
	systemctl --user daemon-reload
	systemctl --user enable ggitree.service
	systemctl --user restart ggitree.service

uninstall:
	-systemctl --user disable --now ggitree.service
	rm -f $(HOME)/.config/systemd/user/ggitree.service $(PREFIX)/bin/ggitree

clean:
	rm -rf ggitree web/dist
