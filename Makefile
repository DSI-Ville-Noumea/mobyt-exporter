NAME := mobyt-exporter

BUILD  := ${CURDIR}/build
BIN    := ${BUILD}/bin/${NAME}

.PHONY: clean tidy compile test run
.DEFAULT_GOAL := build

clean:
	rm -rf ${BUILD}

tidy:
	go mod tidy -v

fmt:
	#go fmt github.com/DSI-Ville-Noumea/mobyt-exporter/...
	go fmt

vendor:
	go mod vendor

compile:
	go build -mod=readonly -o ${BIN}

#test:
#	go test -v ./...

build: tidy fmt vendor compile

run: build
	${BIN}
