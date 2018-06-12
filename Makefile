all: test

PACKAGES := ./...

init:
	go get -u github.com/AlekSi/gocoverutil
	go get -u gopkg.in/alecthomas/gometalinter.v1
	gometalinter.v1 --install

install:
	go install -v $(PACKAGES)
	go test -v -i $(PACKAGES)

install-race:
	go install -v -race $(PACKAGES)
	go test -v -race -i $(PACKAGES)

test:
	go test -v $(PACKAGES)

test-race: install-race
	go test -v -race $(PACKAGES)

test-race-cover: install
	gocoverutil test -v -race $(PACKAGES)

check: install
	-gometalinter.v1 --tests --skip=api --deadline=180s ./...
