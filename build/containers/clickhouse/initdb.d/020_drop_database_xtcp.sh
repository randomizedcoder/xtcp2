#!/bin/bash
# apparently /usr/bin/bash doesn't exist in the container

#
# This is the clickhouse database table creation script for xtcp2
#

set -e;

if [ "$EUID" -ne 0 ]
then
	echo "Please run as root";
	exit 1;
fi

DIR="/docker-entrypoint-initdb.d/";

# https://clickhouse.com/docs/en/interfaces/formats#protobuf
# https://clickhouse.com/docs/en/interfaces/formats#protobufsingle
# https://clickhouse.com/docs/en/interfaces/formats#protobuflist

# https://protobuf.dev/programming-guides/encoding/#structure

# https://clickhouse.com/blog/optimize-clickhouse-codecs-compression-schema

# https://altinity.com/blog/2019-7-new-encodings-to-improve-clickhouse
# https://altinity.com/blog/clickhouse-for-time-series

clickhouse-client -n <<-EOSQL
    SELECT now();

    --------------------------------------------------------------------------------------------------
    -- https://clickhouse.com/docs/en/cloud/bestpractices/asynchronous-inserts
    -- https://medium.com/@kn2414e/utilizing-go-and-clickhouse-for-large-scale-data-ingestion-and-application-146822f7020c
    -- FIX ME!!  Work out how to set this!!
    -- ALTER USER root SETTINGS async_insert = 1;

    --------------------------------------------------------------------------------------------------

    -- Reload protobufs
    -- https://clickhouse.com/docs/en/interfaces/formats#drop-protobuf-cache
    -- SYSTEM DROP FORMAT SCHEMA CACHE FOR Protobuf;

    --------------------------------------------------------------------------------------------------

    DROP DATABASE IF EXISTS xtcp;
    CREATE DATABASE IF NOT EXISTS xtcp;

EOSQL

d=$(date +date_%Y_%m_%d_%H_%M_%S);
du=$(date --utc +date_utc_%Y_%m_%d_%H_%M_%S);

echo "${d}" > "${DIR}out/date_drop";
echo "${du}" > "${DIR}out/date_utc_drop";

# end