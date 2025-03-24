#!/bin/bash
# apparently /usr/bin/bash doesn't exist in the container

#
# Init before creating tables
#

#
# This is the clickhouse database table creation script for xtcp2
#

set -e;

if [ "${EUID}" -ne 0 ]
then
	echo "Please run as root";
	exit 1;
fi

# # Get the full path of the script
# SCRIPT_PATH="${BASH_SOURCE[0]}"
# # Extract the filename without the directory
# SCRIPT_NAME=$(basename "$SCRIPT_PATH")
# # Remove the ".sh" extension
# SCRIPT_NAME_NO_EXT="${SCRIPT_NAME%.*}"

DIR="/docker-entrypoint-initdb.d/";

echo "before";

CMD="ls -la "${DIR}out/"";
echo "${CMD}";
eval "${CMD}";

# rm --recursive --force "${DIR}out/*";
# rm --recursive --force "${DIR}out/date*";
# rm --recursive --force "${DIR}out/date_utc*";
# rm --recursive --force "${DIR}out/whoami";
# rm --recursive --force "${DIR}out/success";
# rm --recursive --force "${DIR}out/xtcp.*";
# rm --recursive --force "${DIR}out/xtcp.xtcp_flat_records*";
# rm --recursive --force "${DIR}out/xtcp.xtcp_flat_records_kafka*";

CMD="rm --recursive --force ${DIR}out/*";
echo "${CMD}";
eval "${CMD}";

echo "after";

CMD="ls -la "${DIR}out/"";
echo "${CMD}";
eval "${CMD}";

d=$(date +date_%Y_%m_%d_%H_%M_%S);
du=$(date --utc +date_utc_%Y_%m_%d_%H_%M_%S);
w=$(whoami);

echo "${d}" > "${DIR}out/date";
echo "${du}" > "${DIR}out/date_utc";
echo "${w}" > "${DIR}out/whoami";

# end