#!/usr/bin/bash

p=$(pwd) || true

sudo rm -rf gen
docker run --volume "${p}:/workspace" --workdir /workspace bufbuild/buf dep update pkg/xtcppb
docker run --volume "${p}:/workspace" --workdir /workspace bufbuild/buf build pkg/xtcppb
docker run --volume "${p}:/workspace" --workdir /workspace bufbuild/buf generate pkg/xtcppb
