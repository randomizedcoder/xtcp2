#
# /cmd/nsTest/Makefile
#

# ldflags variables to update --version
# short commit hash
COMMIT := $(shell git describe --always)
DATE := $(shell date -u +"%Y-%m-%d-%H:%M")
BINARY := nsTest

all: clean build

test:
	go test

clean:
	[ -f ${BINARY} ] && rm -rf ./${BINARY} || true

build:
	go build -ldflags \
		"-X main.commit=${COMMIT} -X main.date=${DATE} -X main.version=${VERSION}" \
		-o ./${BINARY} \
		./${BINARY}.go

run:
	sudo ./delete_network_namespaces.bash
	sudo ./nsTest --sleep 0.1s

ls:
	sudo ls -la "/var/run/netns"
#
