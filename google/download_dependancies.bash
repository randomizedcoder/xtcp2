#!/bin/bash

wget -O ./api/annotations.proto https://raw.githubusercontent.com/googleapis/googleapis/refs/heads/master/google/api/annotations.proto
wget -O ./api/http.proto https://raw.githubusercontent.com/googleapis/googleapis/refs/heads/master/google/api/http.proto
wget -O ./protobuf/duration.proto https://raw.githubusercontent.com/protocolbuffers/protobuf/refs/heads/main/src/google/protobuf/duration.proto
wget -O ./protobuf/timestamp.proto https://raw.githubusercontent.com/protocolbuffers/protobuf/refs/heads/main/src/google/protobuf/timestamp.proto
wget -O ./protobuf/io/coded_stream.h https://raw.githubusercontent.com/protocolbuffers/protobuf/refs/heads/main/src/google/protobuf/io/coded_stream.h
wget -O ./protobuf/generated_message_tctable_impl.h https://raw.githubusercontent.com/protocolbuffers/protobuf/refs/heads/main/src/google/protobuf/generated_message_tctable_impl.h
wget -O ./protobuf/extension_set.h https://raw.githubusercontent.com/protocolbuffers/protobuf/refs/heads/main/src/google/protobuf/extension_set.h
wget -O ./protobuf/generated_message_util.h https://raw.githubusercontent.com/protocolbuffers/protobuf/refs/heads/main/src/google/protobuf/generated_message_util.h
wget -O ./protobuf/wire_format_lite.h https://raw.githubusercontent.com/protocolbuffers/protobuf/refs/heads/main/src/google/protobuf/wire_format_lite.h
wget -O ./protobuf/descriptor.h https://raw.githubusercontent.com/protocolbuffers/protobuf/refs/heads/main/src/google/protobuf/descriptor.h
wget -O ./protobuf/generated_message_reflection.h https://raw.githubusercontent.com/protocolbuffers/protobuf/refs/heads/main/src/google/protobuf/generated_message_reflection.h
wget -O ./protobuf/reflection_ops.h https://raw.githubusercontent.com/protocolbuffers/protobuf/refs/heads/main/src/google/protobuf/reflection_ops.h
wget -O ./protobuf/wire_format.h https://raw.githubusercontent.com/protocolbuffers/protobuf/refs/heads/main/src/google/protobuf/wire_format.h
wget -O ./protobuf/port_def.inc https://raw.githubusercontent.com/protocolbuffers/protobuf/refs/heads/main/src/google/protobuf/port_def.inc