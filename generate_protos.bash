#!/bin/bash

echo "running generate_protos.bash";

p=$(pwd) || true

u=$(id -u)
g=$(id -g)

# sudo rm -rf proto/gen
# mkdir -p proto/gen/go
# mkdir -p proto/gen/openapiv2
# mkdir -p proto/gen/dart
# mkdir -p proto/gen/python
#mkdir -p proto/gen/rust
#mkdir -p proto/gen/bq-schema

# https://grpc-ecosystem.github.io/grpc-gateway/docs/tutorials/introduction/#prerequisites
# go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest
# go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
# go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
#go get github.com/grpc-ecosystem/grpc-gateway/v2@v2.24.0
#go get github.com/bufbuild/protovalidate-go@v0.8.0
#go get github.com/bufbuild/protovalidate-go

# https://github.com/grpc-ecosystem/grpc-gateway
cmd="go get \
    google.golang.org/grpc/cmd/protoc-gen-go-grpc \
    github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway \
    github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2 \
    google.golang.org/protobuf/cmd/protoc-gen-go \
    google.golang.org/grpc/cmd/protoc-gen-go-grpc"
echo "$cmd"
eval "$cmd"

# https://buf.build/docs/concepts/modules-workspaces/#dependency-management

echo bufbuild/buf lint
docker run --user "${u}:${g}" \
    --volume "${p}:/workspace" --workdir /workspace \
    --env BUF_CACHE_DIR='/workspace' \
    bufbuild/buf lint

echo bufbuild/buf dep update
docker run --user "${u}:${g}" \
    --volume "${p}:/workspace" --workdir /workspace \
    --env BUF_CACHE_DIR='/workspace' \
    bufbuild/buf dep update

echo bufbuild/buf build
docker run --user "${u}:${g}" \
    --volume "${p}:/workspace" --workdir /workspace \
    --env BUF_CACHE_DIR='/workspace' \
    bufbuild/buf build

echo bufbuild/buf generate
docker run --user "${u}:${g}" \
    --volume "${p}:/workspace" --workdir /workspace \
    --env BUF_CACHE_DIR='/workspace' \
    bufbuild/buf generate

# protos get mounted into the docker container
# - ${XTCPPATH}/build/containers/clickhouse/format_schemas/:/var/lib/clickhouse/format_schemas/:z
# cmd="cp ./proto/xtcp_flat_record/v1/xtcp_flat_record.proto ./build/containers/clickhouse/format_schemas/"
# echo "$cmd"
# eval "$cmd"

echo "recommend running check_protos.bash";

# end