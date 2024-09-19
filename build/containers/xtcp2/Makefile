#
# ./build/containers/xtcp2/Makefile
#
XTCPPATH = /home/das/Downloads/xtcp2
PWD = $(shell pwd)
COMMIT := $(shell git describe --always)
DATE := $(shell date -u +"%Y-%m-%d-%H:%M")
VERSION_FILE := $(shell cat ${XTCPPATH}/VERSION)

buildd:
	docker build \
		-f ./Containerfile \
		--build-arg XTCPPATH=${XTCPPATH} \
		--build-arg PWD=${PWD} \
		--build-arg COMMIT=${COMMIT} \
		--build-arg DATE=${DATE} \
		--build-arg VERSION=${VERSION} \
		--tag xtcp2 \
		${XTCPPATH}