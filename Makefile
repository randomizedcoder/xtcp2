#
# /xtcp2/Makefile
#

# Execute "make build_and_deploy"

# This make file will build all the nessisary containers for a local deployment of
# - xtcp ( extracts socket data )
# - redpanda ( kafka pub/sub system )
# - clickhouse data store

VERSION := $(shell cat VERSION)
LOCAL_MAJOR_VERSION := $(word 1,$(subst ., ,$(VERSION_FILE)))
LOCAL_MINOR_VERSION := $(word 2,$(subst ., ,$(VERSION_FILE)))
LOCAL_PATCH_VERSION := $(word 3,$(subst ., ,$(VERSION_FILE)))
SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c

XTCPPATH = $(shell pwd)
COMMIT := $(shell git describe --always)
DATE := $(shell date -u +"%Y-%m-%d-%H:%M")

.PHONY: build

build_and_deploy: builddocker deploy

# https://docs.docker.com/engine/reference/commandline/docker/
# https://docs.docker.com/compose/reference/
deploy:
	@echo "================================"
	@echo "Make deploy"
	echo XTCPPATH=${XTCPPATH}
	XTCPPATH=${XTCPPATH} \
	docker compose \
		--file build/containers/redpanda/docker-compose.yml \
		up -d --remove-orphans

down:
	@echo "================================"
	@echo "Make down"
	XTCPPATH=${XTCPPATH} \
	docker compose \
	--file build/containers/redpanda/docker-compose.yml \
	down

#--env-file docker-compose-enviroment-variables \

builddocker: builddocker_xtcp builddocker_clickhouse

builddocker_xtcp:
	@echo "================================"
	@echo "Make builddocker_xtcp"
	docker build \
		--build-arg XTCPPATH=${XTCPPATH} \
		--build-arg COMMIT=${COMMIT} \
		--build-arg DATE=${DATE} \
		--build-arg VERSION=${VERSION} \
		--file build/containers/xtcp2/Containerfile \
		--tag "xtcp:${VERSION}" --tag xtcp:latest \
		${XTCPPATH}

builddocker_clickhouse:
	@echo "================================"
	@echo "Make builddocker_clickhouse"
	docker build \
		--build-arg XTCPPATH=${XTCPPATH} \
		--build-arg VERSION=${VERSION} \
		--file build/containers/clickhouse/Containerfile \
		--tag xtcp_clickhouse:${VERSION} --tag xtcp_clickhouse:latest \
		.

update_dependancies:

	go get -u golang.org/x/time@latest
	go get -u golang.org/x/sys@latest

	go get -u google.golang.org/grpc@latest
	go get -u google.golang.org/protobuf@latest

	go get -u github.com/pkg/profile@latest
	go get -u github.com/prometheus/client_golang@latest

	go get -u github.com/nats-io/nats.go@latest
	go get -u github.com/nsqio/go-nsq@latest
	go get -u github.com/twmb/franz-go@latest
	go get -u github.com/twmb/franz-go/plugin/kprom@latest
	go get -u github.com/vmihailenco/msgpack/v5@latest

	go mod verify
	go mod tidy
#go mod vendor

test:
	go test -v ./pkg/xtcpnl/

bench:
	go test -bench=. ./pkg/xtcpnl/

followxtcp:
	docker logs xtcp-xtcp2-1 --follow

ch:
	docker exec -it xtcp-clickhouse-1 bash

ch_prom:
	curl --silent http://localhost:9363/metrics | grep -v "#" | grep -i kafka

clear_docker_volumes:
	docker volume rm redpanda-quickstart-one-broker_redpanda-0 || true
	docker volume rm redpanda_redpanda-0 || true
	docker volume rm xtcp_nsq_data || true
	docker volume rm xtcp_redpanda-0 || true

	docker volume ls

# end