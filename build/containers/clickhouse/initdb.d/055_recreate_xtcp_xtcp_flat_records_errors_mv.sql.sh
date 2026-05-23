#!/bin/bash
# apparently /usr/bin/bash doesn't exist in the container

#
# Capture Kafka-engine parse errors into xtcp.xtcp_flat_records_errors via
# xtcp.xtcp_flat_records_errors_mv. Must run after the kafka engine table
# (040) and the main MV (050) so the source table's virtual _error column
# is in scope.
#

set -e;

if [ "${EUID}" -ne 0 ]
then
	echo "Please run as root";
	exit 1;
fi

CLICKHOUSE_CLIENT="clickhouse-client";

DIR="/docker-entrypoint-initdb.d/";

SQL_FILE="${DIR}sql/xtcp_xtcp_flat_records_errors_mv.sql";

CMD="${CLICKHOUSE_CLIENT} --time < ${SQL_FILE}";

echo "${CMD}";
eval "${CMD}";

d=$(date +date_%Y_%m_%d_%H_%M_%S);
du=$(date --utc +date_utc_%Y_%m_%d_%H_%M_%S);

echo "${d}" > "${DIR}out/date_errors_mv";
echo "${du}" > "${DIR}out/date_utc_errors_mv";

# end
