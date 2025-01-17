#!/bin/bash

# Ensure the script is run as root
if [ "$(id -u)" -ne 0 ]; then
    echo "This script must be run as root."
    exit 1
fi

# Directory where network namespaces are listed
NETNS_DIR="/run/netns"

# Check if the directory exists
if [ ! -d "$NETNS_DIR" ]; then
    echo "No network namespace directory found at $NETNS_DIR."
    exit 0
fi

# List all namespaces and remove them
# shellcheck disable=SC2045
for ns in $(ls "$NETNS_DIR"); do
    echo "ns:$ns"
    if [[ "$ns" =~ ^ns ]]; then
        echo "Removing network namespace: $ns"
        ip netns del "$ns"
        if [ $? -eq 0 ]; then
            echo "Successfully removed: $ns"
        else
            echo "Failed to remove: $ns"
        fi
    fi
done

echo "All network namespaces removed."
