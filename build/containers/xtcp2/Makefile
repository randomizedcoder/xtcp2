#
# ./build/containers/xtcp2/Makefile
#
XTCPPATH = /home/das/Downloads/xtcp2
PWD = $(shell pwd)
COMMIT := $(shell git describe --always)
DATE := $(shell date -u +"%Y-%m-%d-%H:%M")
VERSION := $(shell cat ${XTCPPATH}/VERSION)

buildd:
	docker build \
		-f ./Containerfile \
		--build-arg XTCPPATH=${XTCPPATH} \
		--build-arg PWD=${PWD} \
		--build-arg COMMIT=${COMMIT} \
		--build-arg DATE=${DATE} \
		--build-arg VERSION=${VERSION} \
		--tag randomizedcoder/xtcp2:${VERSION} \
		${XTCPPATH}

pushd:
	docker login
	docker push randomizedcoder/xtcp2:${VERSION}

# [das@t:~/Downloads/xtcp2/build/containers/xtcp2]$ docker push randomizedcoder/xtcp2:2.0.1
# The push refers to repository [docker.io/randomizedcoder/xtcp2]
# bf85b8c9ed2f: Pushed
# b336e209998f: Pushed
# f4aee9e53c42: Pushed
# 1a73b54f556b: Pushed
# 2a92d6ac9e4f: Pushed
# bbb6cacb8c82: Pushed
# 6f1cdceb6a31: Pushed
# af5aa97ebe6c: Pushed
# 4d049f83d9cf: Pushed
# a80545a98dcd: Pushed
# 8fa10c0194df: Pushed
# 03af25190641: Pushed
# 2.0.1: digest: sha256:2777d2b009a49cfbf8c56b458f720a05c1ad2d4d70c75bf86a483d217f6ff4c3 size: 2814

#https://docs.docker.com/reference/cli/docker/buildx/build/#platform
#--platform linux/amd64,linux/arm64,linux/arm/v7,linux/aarch64 \
# --progress=plain .
buildx:
	docker buildx build \
		-f ./Containerfile \
		--platform linux/amd64,linux/arm64 \
		--build-arg XTCPPATH=${XTCPPATH} \
		--build-arg PWD=${PWD} \
		--build-arg COMMIT=${COMMIT} \
		--build-arg DATE=${DATE} \
		--build-arg VERSION=${VERSION} \
		--tag randomizedcoder/xtcp2:${VERSION} \
		${XTCPPATH}

#end