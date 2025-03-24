#!/bin/bash
# apparently /usr/bin/bash doesn't exist in the container

#
# This is the clickhouse database table creation script for xtcp2
#

set -e;

if [ "${EUID}" -ne 0 ]
then
	echo "Please run as root";
	exit 1;
fi

CLICKHOUSE_CLIENT="clickhouse-client";

DIR="/docker-entrypoint-initdb.d/";

SQL_FILE="${DIR}sql/show_tables_from_xtcp.sql";

OUT_FILE="${DIR}out/show_tables";

CMD="${CLICKHOUSE_CLIENT} --time < ${SQL_FILE} > ${OUT_FILE}";
echo "${CMD}";
eval "${CMD}";

CMD="cat '/docker-entrypoint-initdb.d/out/show_tables'";
echo "${CMD}";
eval "${CMD}";

d=$(date +date_%Y_%m_%d_%H_%M_%S);
du=$(date --utc +date_utc_%Y_%m_%d_%H_%M_%S);

echo "${d}" > "${DIR}out/date_show_tables";
echo "${du}" > "${DIR}out/date_utc_show_tables";

# end