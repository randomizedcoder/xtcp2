#
# /cmd/xtcp2/Makefile
#

# ldflags variables to update --version
# short commit hash
COMMIT := $(shell git describe --always)
DATE := $(shell date -u +"%Y-%m-%d-%H:%M")
VERSION := $(shell cat ../../VERSION)
BINARY := xtcp2

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

metrics:
	curl -s http://[::1]:9009/metrics 2>&1 | grep -v "#"

nlmon:
	# create the network interface
	sudo ip link add nlmon0 type nlmon
	sudo ip link set dev nlmon0 up
	sudo tcpdump -i nlmon0 -s0 -w nlmon_capture.pcap -v

netem:
	sudo tc qdisc add dev lo root netem delay 100ms 50ms 30% reorder 25% 50% duplicate 2% 4% corrupt 1% loss 4% 30% limit 100000
	sudo tc qdisc show dev lo
	sudo tc qdisc del dev lo root netem

#sudo tc qdisc add dev lo root netem delay 50ms 10ms 30% reorder 25% 50% duplicate 1% corrupt 0.2% loss 1% 30% limit 100000
#[das@vm3:~]$ sudo tc qdisc show dev lo
#qdisc netem 8002: root refcnt 2 limit 100000 delay 50ms  10ms 30% loss 1% 30% duplicate 1% reorder 25% 50% corrupt 0.2% seed 18224122293576613729 gap 1


ss:
	ss --tcp --info -n -o state established --extended --memory --tos --processes --cgroup

	ss --tcp --info -n -o state connected --extended --memory --tos --processes --cgroup

	ss --tcp --info -n -a --extended --memory --tos --processes --cgroup --context --options

profile:
	timeout 30m ./xtcp2 --profile.mode cpu

analyze:
	go tool pprof -web xtcp2 cpu.pprof

analyzeWeb:
	go tool pprof -http=localhost:9010 xtcp2 cpu.pprof

exited:
	docker container ls -f status=exited -a

checkenvvar:
	MAXLOOPS=10 DEST=BOO ./${BINARY} -conf

#
