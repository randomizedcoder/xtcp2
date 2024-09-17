#
# ./build/containers/redpanda/Makefile
#
all:
	docker compose up

# https://hub.docker.com/r/redpandadata/redpanda

# https://docs.redpanda.com/current/get-started/quick-start/?tab=tabs-1-single-broker

get_compose:
	wget https://docs.redpanda.com/redpanda-labs/docker-compose/_attachments/single-broker/docker-compose.yml


# https://docs.redpanda.com/current/get-started/quick-start/
# https://github.com/redpanda-data/redpanda/tags
# https://github.com/redpanda-data/console/tags
dc:
	REDPANDA_VERSION=24.1.16 REDPANDA_CONSOLE_VERSION=2.7.2 docker compose up --detach --force-recreate

# docker exec -it redpanda-0 rpk topic create xtcp
#

up:
	podman-compose up

# https://hellokube.dev/posts/three-ways-zookeepeerless-kafka/
kakfa:
	docker run -it \
		--replace \
		--name kafka-zkless \
		-p 9092:9092 \
		-e LOG_DIR=/tmp/logs \
		quay.io/strimzi/kafka:latest-kafka-2.8.1-amd64 \
		/bin/sh -c 'export CLUSTER_ID=$(uuidgen) && bin/kafka-storage.sh format --cluster-id $CLUSTER_ID $(bin/kafka-storage.sh random-uuid) -c config/kraft/server.properties && bin/kafka-server-start.sh config/kraft/server.properties'

# https://docs.redpanda.com/current/get-started/rpk-install/
# curl -LO https://github.com/redpanda-data/redpanda/releases/latest/download/rpk-linux-amd64.zip

register:
	./rpk -X registry.hosts=localhost:18081 registry schema delete xtcp_record --schema-version 1
	./rpk -X registry.hosts=localhost:18081 registry schema create xtcp_record --schema ../../../pkg/xtcppb/xtcppb.proto
	./rpk -X registry.hosts=localhost:18081 registry schema get --id 1
	./rpk -X registry.hosts=localhost:18081 registry subject list --format json
	curl -s "http://localhost:18081/schemas/types" | jq .
	curl -s "http://localhost:18081/subjects" | jq .
	curl -s "http://localhost:18081/subjects/xtcp_record/versions/1" | jq .
	curl -s "http://localhost:18081/subjects/xtcp_record/versions/latest" | jq .

rp:
	http://localhost:8080/

# rpk source
# https://github.com/redpanda-data/redpanda/tree/dev/src/go/rpk

# end
