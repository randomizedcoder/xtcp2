#
# /cmd/ns/Makefile
#

# ldflags variables to update --version
# short commit hash
COMMIT := $(shell git describe --always)
DATE := $(shell date -u +"%Y-%m-%d-%H:%M")
BINARY := ns

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
	sudo GOGC=50 ./${BINARY}

metrics:
	curl -s http://localhost:9010/metrics 2>&1 | grep -v '#'

#
