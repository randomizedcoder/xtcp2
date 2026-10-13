# nix/microvms/self-test-jsonl-tally.nix
#
# The `jsonl_tally` bash function the self-test uses to validate jsonl
# records, as a string so nix/checks/self-test-jsonl-tally.nix can drive the
# exact same text without a VM.
#
# Checks 5d (OUTPUT_CONTENT) and 5f (RAW_SOCKET) used to fork one jq per line.
# The daemon's jsonl holds every record since boot, so on a loaded host the
# file was longer by the time 5d ran and each fork was slower too: 5d took
# 325 s for 788 records at load ~45, and stalled past 600 s on ~1640. One jq
# pass over the whole input does 20,000 lines in under a second.
#
#   jsonl_tally < lines
#
# Prints "<good> <bad>". Empty lines are skipped. A line is good if it parses
# as one JSON object with a non-empty hostname (protojson stamps hostname on
# every record); anything else, including two objects on one line, is bad. If
# jq itself fails it prints nothing, and the callers' `${good:-0}` then
# fails the check rather than passing it.
#
# Needs jq.
''
  jsonl_tally() {
    jq -nRr '[inputs | select(length > 0)
              | try (fromjson | has("hostname") and (.hostname | length > 0)) catch false]
             | "\(map(select(.)) | length) \(map(select(. | not)) | length)"'
  }
''
