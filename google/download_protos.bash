#!/bin/bash

wget -O ./api/annotations.proto https://raw.githubusercontent.com/googleapis/googleapis/refs/heads/master/google/api/annotations.proto

wget -O ./api/http.proto https://raw.githubusercontent.com/googleapis/googleapis/refs/heads/master/google/api/http.proto

wget -O ./protobuf/duration.proto https://raw.githubusercontent.com/protocolbuffers/protobuf/refs/heads/main/src/google/protobuf/duration.proto

wget -O ./protobuf/timestamp.proto https://raw.githubusercontent.com/protocolbuffers/protobuf/refs/heads/main/src/google/protobuf/timestamp.proto
