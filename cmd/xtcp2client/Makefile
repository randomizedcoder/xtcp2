#
# /cmd/xtcp2client/Makefile
#

# ldflags variables to update --version
# short commit hash
COMMIT := $(shell git describe --always)
DATE := $(shell date -u +"%Y-%m-%d-%H:%M")
BINARY := xtcp2client

all: clean build

test:
	go test

clean:
	[ -f ${BINARY} ] && rm -rf ./${BINARY} || true

build:
	go build -ldflags \
		"-X main.commit=${COMMIT} -X main.date=${DATE} -X main.date=${VERSION}" \
		-o ./${BINARY} \
		./${BINARY}.go

metrics:
	curl -s http://[::1]:9009/metrics 2>&1 | grep -v "#"

profile:
	timeout 30m ./xtcp2client --profile.mode cpu

analyze:
	go tool pprof -web xtcp2 cpu.pprof

analyzeWeb:
	go tool pprof -http=localhost:9010 xtcp2 cpu.pprof

#
