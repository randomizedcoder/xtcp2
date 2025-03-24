#!/bin/bash
#
# check_protos.bash
#

echo "running check_protos.bash";

compare_files() {
  local file1="$1";
  local file2="$2";

  if [ $# -ne 2 ]; then
    echo "Usage: compare_files <file1> <file2>";
    return 1;
  fi

  if [ ! -f "${file1}" ]; then
    echo "Error: File '${file1}' not found.";
    return 1;
  fi

  if [ ! -f "${file2}" ]; then
    echo "Error: File '${file2}' not found.";
    return 1;
  fi

  echo "compare_files ${file1} ${file2}";

	diff_result=$(diff "${file1}" "${file2}"; echo $?);

	if [ "${diff_result}" -eq 0 ]; then
		echo "diff_result is zero: ${file1}:${file2}";
	else
		sudo chown -R das:users ./build/containers/clickhouse/format_schemas/;
		cp "${file1}" "${file2}";
		echo "Files differ. Copied ${file1}:${file2}";
	fi
}

file1="./proto/xtcp_flat_record/v1/xtcp_flat_record.proto";
file2="./build/containers/clickhouse/format_schemas/xtcp_flat_record.proto";

compare_files "${file1}" "${file2}";

file1="./proto/clickhouse_protolist/v1/clickhouse_protolist.proto";
file2="./build/containers/clickhouse/format_schemas/clickhouse_protolist.proto";

compare_files "${file1}" "${file2}";

# end