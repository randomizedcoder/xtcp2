#!/bin/bash

base_namespace_name="ns"
initial_namespaces=1000
sleep_duration=0.1

# Create a namespace
create_namespace() {
    local name=$1
    echo "Creating namespace: $name"
    ip netns add "$name"
    if [[ $? -ne 0 ]]; then
        echo "Failed to create namespace: $name"
    fi
}

# Remove a namespace
remove_namespace() {
    local name=$1
    echo "Removing namespace: $name"
    ip netns del "$name"
    if [[ $? -ne 0 ]]; then
        echo "Failed to remove namespace: $name"
    fi
}

# Generate namespace name
namespace_name() {
    local index=$1
    echo "${base_namespace_name}${index}"
}

# Create initial namespaces
for ((i=0; i<initial_namespaces; i++)); do
    create_namespace "$(namespace_name $i)"
done

# Manage namespaces in a loop
j=0
while true; do
    echo "j: $j"

    oldest_namespace="$(namespace_name $j)"
    remove_namespace "$oldest_namespace"
    echo "Removed namespace: $oldest_namespace"

    new_namespace="$(namespace_name $((j + initial_namespaces)))"
    create_namespace "$new_namespace"
    echo "Added namespace: $new_namespace"
    ((j++))

    sleep $sleep_duration
done