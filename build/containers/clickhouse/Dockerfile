#
#
#

#-f ./build/containers/$*/Dockerfile --tag

# https://hub.docker.com/r/clickhouse/clickhouse-server/tags
FROM clickhouse/clickhouse-server:24.3.5.46-alpine

ARG USER_UID=101
ARG GROUP_GID=101

COPY --chown=${USER_UID}:${GROUP_GID} --chmod=544 ./xtcppb.proto /var/lib/clickhouse/format_schemas/xtcppb.proto

