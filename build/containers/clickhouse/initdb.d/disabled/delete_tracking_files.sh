#!/bin/bash
#
# This script for deleting temp files from the real host, rather than the container
#
set -o xtrace

DIR=${PWD}"/";

rm --recursive --force "${DIR}"date*;
rm --recursive --force "${DIR}"date_utc*;
rm --recursive --force "${DIR}"whoami;
rm --recursive --force "${DIR}"success;
rm --recursive --force "${DIR}"xtcp.*;
rm --recursive --force "${DIR}"xtcp.xtcp_flat_records*;
rm --recursive --force "${DIR}"xtcp.xtcp_flat_records_kafka*;

# end