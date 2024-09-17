#
# ./build/containers/clickhouse/Makefile
#

# https://www.redhat.com/sysadmin/compose-podman-pods

PWD = $(shell pwd)

buildd:
	docker build -f ./Containerfile --tag xtcp_clickhouse .

buildp:
	podman build -f ./Containerfile --tag xtcp_clickhouse .

dc:
	docker compose up --detach --force-recreate

runCH:
	podman run \
		--name ch \
		--interactive \
		--tty \
		--rm \
		--replace \
		--detach \
		--annotation run.oci.keep_original_groups=1 \
		--group-add keep-groups \
		--userns=keep-id \
		--ulimit nofile=262144:262144 \
		--ulimit memlock="-1:-1" \
		--cap-add=SYS_NICE \
		--cap-add=IPC_LOCK \
		--cap-add=NET_ADMIN \
		-e CLICKHOUSE_ALWAYS_RUN_INITDB_SCRIPTS=true \
		-p 8123:8123 \
		-p 9001:9000 \
		-p 9005:9005 \
		-v ./db/:/var/lib/clickhouse/:Z \
		-v ./initdb.d/:/docker-entrypoint-initdb.d/:Z \
		-v ./logs/:/var/log/clickhouse-server/:Z \
		-v das:/home/das/:Z,U \
		xtcp_clickhouse
	@echo --------------------------
	podman ps
	@echo --------------------------
	@echo podman exec --interactive --tty ch bash
	@echo podman exec --interactive --tty ch clickhouse-client

runCHnd:
	podman run \
		--name ch \
		--interactive \
		--tty \
		--rm \
		--replace \
		--annotation run.oci.keep_original_groups=1 \
		--group-add keep-groups \
		--userns=keep-id \
		--ulimit nofile=262144:262144 \
		--ulimit memlock="-1:-1" \
		--cap-add=SYS_NICE \
		--cap-add=IPC_LOCK \
		--cap-add=NET_ADMIN \
		-e CLICKHOUSE_ALWAYS_RUN_INITDB_SCRIPTS=true \
		-p 8123:8123 \
		-p 9001:9000 \
		-p 9005:9005 \
		-v ./db/:/var/lib/clickhouse/:Z \
		-v ./initdb.d/:/docker-entrypoint-initdb.d/:Z \
		-v ./logs/:/var/log/clickhouse-server/:Z \
		-v das:/home/das/:Z,U \
		xtcp_clickhouse

# http
#- 8123:8123
# native
#- 9001:9000
# postgres
#- 9005:9005
# https://clickhouse.com/docs/en/guides/sre/network-ports

# https://www.redhat.com/sysadmin/rootless-podman-user-namespace-modes

# -e HOME=/tmp \

# SYSTEM DROP FORMAT SCHEMA CACHE FOR Protobuf

# example of writing protobuf to kafka
# https://github.com/ClickHouse/ClickHouse/issues/59029

# https://clickhouse.com/docs/knowledgebase/configure_cap_ipc_lock_and_cap_sys_nice_in_docker

timestamp:
	wget https://raw.githubusercontent.com/protocolbuffers/protobuf/main/src/google/protobuf/timestamp.proto

# end
