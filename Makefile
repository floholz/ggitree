.PHONY: build web test run install clean

build: web
	go build -o ggitree .

web:
	go generate ./web

test:
	go test ./...

run: build
	./ggitree serve .

install: web
	go install .

clean:
	rm -rf ggitree web/dist
