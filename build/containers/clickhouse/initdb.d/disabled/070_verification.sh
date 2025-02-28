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

#-----------------------------------
# This code does a quick check that the tables for the Kafka and the real output
# table match.  The tables NEED to match.

if [ ! -f /usr/bin/sha512sum ]; then
	echo "/usr/bin/sha512sum not found";
	exit 1;
fi

DIR="/docker-entrypoint-initdb.d/";

DATE=$(cat ${DIR}out/date);

file1="${DIR}out/xtcp.xtcp_flat_records_kafka_${DATE}";
file2="${DIR}out/xtcp.xtcp_flat_records_${DATE}";

sha512sum1=$(sha512sum "${file1}" | cut -d ' ' -f 1);
sha512sum2=$(sha512sum "${file2}" | cut -d ' ' -f 1);

if [ "${sha512sum1}" != "${sha512sum2}" ]; then
	echo "DESCRIBE TABLES DO NOT MATCH!!  Fix the tables!!";
	exit 1;
fi

if [ "${sha512sum1}" == "${sha512sum2}" ]; then
	echo "DESCRIBE TABLES MATCH.  Woot woot!";
fi

d=$(date +date%Y_%m_%d_%H_%M_%S);
du=$(date --utc +date_utc_%Y_%m_%d_%H_%M_%S);

echo "${d}" > "${DIR}out/success";
echo "success:${d}";

echo "${d}" > "${DIR}out/date_verification";
echo "${du}" > "${DIR}out/date_utc_verification";

# end