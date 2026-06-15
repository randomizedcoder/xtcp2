#
#
#

#-f ./build/containers/$*/Dockerfile --tag

# https://hub.docker.com/r/clickhouse/clickhouse-server/tags
# Bumped 2026-05-21: 24.3.5.46 → 25.3.5.34-alpine. ClickHouse ships
# an LTS minor every quarter; 25.3 was the first LTS of the 25.x line.
# Verify the tag exists with
#   docker manifest inspect clickhouse/clickhouse-server:25.3.5.34-alpine
# before relying on it.
FROM clickhouse/clickhouse-server:25.3.5.34-alpine

ARG USER_UID=101
ARG GROUP_GID=101

COPY --chown=${USER_UID}:${GROUP_GID} --chmod=544 ./xtcppb.proto /var/lib/clickhouse/format_schemas/xtcppb.proto

