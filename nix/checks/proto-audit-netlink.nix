# nix/checks/proto-audit-netlink.nix
#
# The netlink LAYOUT ORACLE.
#
# Every other netlink test in this repo asks "does the decoder read this Go
# struct the way we think it does?". None of them can ask "is this Go struct
# the shape the kernel actually sends?", because they all start from the struct.
# That blind spot is not hypothetical: 30 binary.Read reflection twins agreed
# happily across the 11-field Accurate ECN block that `struct tcp_info` grew in
# kernel 7.0 and xtcp2 was silently truncating. See TODO-SOON.md §21.
#
# This check closes it by comparing xtcp2's structs against the Linux kernel
# UAPI headers — indexed by WIRE BIT OFFSET, not by field name, so a field that
# exists under a different spelling still matches and a field that does not
# exist at all is reported missing.
#
# Two commands, two different questions:
#
#   audit    — static: does the Go struct agree with the kernel headers?
#   validate-netlink — dynamic: replaying xtcp2's own captured pcaps through a
#                      generated dissector, do the decoded values line up?
#                      Grades each protocol Gold/Silver/Bronze.
#
# GATING PER PROTOCOL, not all-or-nothing — see `gatedProtocols` below. A
# protocol named there fails the build on any unallowlisted delta; every other
# protocol is advisory, its findings written to $out and printed in full, exit
# 0. Today the list is just NL_Diag_TCPInfo, the one protocol whose deltas are
# fully triaged. The remaining 17 hold 179 untriaged deltas between them, which
# is why a blanket flip would make the check permanently red and therefore
# permanently ignored; each later phase gates the protocol it covers.
#
# Two outputs, so the two are never confused:
#   $out/unallowlisted.json        every unallowlisted delta (advisory)
#   $out/unallowlisted-gated.json  the gated subset (non-empty ⇒ build fails)
#
# THE WIRING DETAIL THAT MATTERS: proto-audit defaults --xtcp2-src to a
# fetchFromGitHub snapshot pinned inside xdp2's own flake
# (xdp2/nix/proto-audit-sources.nix:224). That snapshot is stale — rev
# a52e2f46, dated 2025-04-19 and 732 commits behind xtcp2 main. It predates the
# whole netlink effort. Left at the default this check would audit a
# year-old copy of xtcp2 and cheerfully report the bug it exists to catch. So
# both paths are overridden to *this* tree below, and that override is the
# single most important line in the file. See nix/upstream-pins.json.
#
# KNOWN UPSTREAM LIMITATION, because it changes how to read the output: even
# with the override, proto-audit reports the 11 Accurate ECN fields as missing
# from NL_Diag_TCPInfo. That is a bug in xdp2's protocol registry, not a gap in
# pkg/xtcpnl — the registry hardcodes `.xtcp2("TCPInfo6_10_3")`, a 248-byte
# struct that predates AccECN, so the `type TCPInfo TCPInfo7_0_3` alias is
# never followed. The 11 are allowlisted as kind=upstream-registry-pin with the
# full diagnosis in proto-audit-netlink-allowlist.json.
#
{
  pkgs,
  lib,
  src,
  xdp2,
  # Protocols whose unallowlisted deltas FAIL the build. Everything not named
  # here stays advisory: its deltas are printed in full and exit 0.
  #
  # A list rather than a boolean because scope is the whole difficulty. A
  # blanket flip is one line, but $out/unallowlisted.json currently holds 179
  # deltas across 18 protocols, none of them triaged, so turning it on wholesale
  # makes the check permanently red and therefore ignored. Gating is earned one
  # protocol at a time: triage that protocol's deltas into the allowlist with
  # reasons, get it to zero, then add its name here. Each later phase gates the
  # protocol it covers.
  gatedProtocols ? [ ],
}:

let
  protoAudit = xdp2.packages.${pkgs.stdenv.hostPlatform.system}.proto-audit;

  allowlist = ./proto-audit-netlink-allowlist.json;

  # The netlink protocols proto-audit has registered. Kept explicit rather than
  # discovered so that a protocol disappearing upstream is a visible diff here
  # rather than silently shrinking the audit.
  protocols = [
    "NL_Addr"
    "NL_Bridge_Port"
    "NL_DCB"
    "NL_Diag_BBRInfo"
    "NL_Diag_DCTCPInfo"
    "NL_Diag_Inet"
    "NL_Diag_MemInfo"
    "NL_Diag_Netlink"
    "NL_Diag_PragueInfo"
    "NL_Diag_ReqV2"
    "NL_Diag_SkMemInfo"
    "NL_Diag_SockID"
    "NL_Diag_TCPInfo"
    "NL_Diag_Unix"
    "NL_Diag_VegasInfo"
    "NL_IfStats"
    "NL_Link"
    "NL_Neigh"
    "NL_Netfilter"
    "NL_Nexthop"
    "NL_Prefix"
    "NL_Route"
    "NL_Rule"
    "NL_TC"
    "NL_XFRM_Policy"
    "NL_XFRM_SA"
  ];

  protoList = lib.concatStringsSep "," protocols;

  unknownGated = lib.subtractLists protocols gatedProtocols;

  # A gated name that is not in `protocols` would never be audited, so its
  # deltas would be zero for the wrong reason and the gate would be silently
  # inert. Fail at eval rather than build: it is a typo, not a finding.
  gatedJSON =
    assert lib.assertMsg (unknownGated == [ ]) (
      "proto-audit-netlink: gatedProtocols names protocol(s) that are not audited: "
      + lib.concatStringsSep ", " unknownGated
    );
    builtins.toJSON gatedProtocols;

  anyGated = gatedProtocols != [ ];

  # Pre-rendered so the shell body below interpolates a short identifier rather
  # than a multi-line expression; nixfmt re-indents the whole script otherwise.
  gatedNames = lib.concatStringsSep " " gatedProtocols;
  gatedLabel = if anyGated then gatedNames else "(none — fully advisory)";
in
pkgs.runCommand "xtcp2-proto-audit-netlink"
  {
    nativeBuildInputs = [
      protoAudit
      pkgs.jq
      pkgs.util-linux # `column`, for the summary table
    ];

    # Overriding both is what points the audit at this tree instead of the
    # stale snapshot pinned in xdp2's flake. `src` is this repository.
    PROTO_AUDIT_XTCP2_SRC = src;
    PROTO_AUDIT_XTCP2_PCAPS = "${src}/pkg/xtcpnl/testdata";

    passthru = {
      inherit gatedProtocols protocols;
    };

    meta = {
      description = "Compare xtcp2's netlink structs against the kernel UAPI headers by wire bit offset";
    };
  }
  ''
    mkdir -p $out

    echo "proto-audit xtcp2 source : $PROTO_AUDIT_XTCP2_SRC"
    echo "proto-audit pcap corpus  : $PROTO_AUDIT_XTCP2_PCAPS"
    echo "protocols                : ${toString (builtins.length protocols)}"
    echo "gated protocols          : ${gatedLabel}"
    echo

    # Guard the override rather than trusting it. If the source path is not
    # this tree, every finding below is about some other code and the check is
    # worse than useless — it is confidently misleading.
    if [ ! -d "$PROTO_AUDIT_XTCP2_PCAPS" ]; then
      echo "FAIL: pcap corpus not found at $PROTO_AUDIT_XTCP2_PCAPS" >&2
      exit 1
    fi

    tcpinfo="$PROTO_AUDIT_XTCP2_SRC/pkg/xtcpnl/xtcpnl_inet_diag_tcpinfo.go"
    if [ ! -f "$tcpinfo" ]; then
      echo "FAIL: $PROTO_AUDIT_XTCP2_SRC does not look like the xtcp2 tree" >&2
      exit 1
    fi

    # Existence of that file proves nothing: the stale snapshot proto-audit
    # defaults to (xdp2's xtcp2Rev, dated 2025-04-19 and 732 commits behind
    # main) has it too. What that snapshot does NOT have is TCPInfo7_0_3, so
    # grep for the thing that actually distinguishes current xtcp2 from the
    # default. Without this, a silently-failed override reads as a clean run
    # and the oracle re-reports the AccECN gap it exists to prove closed.
    # See nix/upstream-pins.json.
    if ! grep -q 'TCPInfo7_0_3' "$tcpinfo"; then
      echo "FAIL: TCPInfo7_0_3 not found in $tcpinfo." >&2
      echo "      The --xtcp2-src override did not take, so proto-audit is" >&2
      echo "      auditing a stale snapshot rather than this tree." >&2
      exit 1
    fi

    cp ${allowlist} $out/allowlist.json

    # --- static: struct layout vs the kernel headers ---------------------
    proto-audit audit --protos '${protoList}' --json \
      > $out/audit.json 2> $out/audit.stderr || true
    proto-audit audit --protos '${protoList}' \
      > $out/audit.txt 2>/dev/null || true

    # --- dynamic: replay this repo's own captures ------------------------
    proto-audit validate-netlink --proto all --json \
      > $out/validate.json 2> $out/validate.stderr || true
    proto-audit validate-netlink --proto all \
      > $out/validate.txt 2>/dev/null || true

    # audit.json is an array of AuditResult (xdp2 samples/proto_audit/src/ir.rs:425):
    #   protocol, total_fields, fields_agree, fields_type_differ,
    #   fields_mismatch, fields_missing, field_comparisons[], validation_tier
    if ! jq -e 'type == "array"' $out/audit.json >/dev/null 2>&1; then
      echo "WARNING: audit.json is not a JSON array; proto-audit likely failed." >&2
      echo "--- stderr ---" >&2
      cat $out/audit.stderr >&2 || true
      # Fully-advisory mode tolerates this; as soon as ANY protocol is gated it
      # must not, or a crashing oracle reads as a clean one — we cannot verify
      # a gated protocol from a run that produced no findings at all.
      ${lib.optionalString anyGated ''
        echo "FAIL: ${toString (builtins.length gatedProtocols)} gated protocol(s) require a parseable audit.json" >&2
        exit 1
      ''}
      exit 0
    fi

    echo "=== layout summary ==="
    jq -r '
      ["protocol","fields","agree","type_differ","mismatch","missing","tier"],
      (.[] | [
        .protocol,
        (.total_fields // 0),
        (.fields_agree // 0),
        (.fields_type_differ // 0),
        (.fields_mismatch // 0),
        (.fields_missing // 0),
        (.validation_tier // "unvalidated")
      ])
      | @tsv
    ' $out/audit.json | column -t || cat $out/audit.json

    # Deltas that are NOT covered by the allowlist. Matched on protocol +
    # offset_bits + field name, so a field shifting offset stops being
    # allowlisted and resurfaces here — the offset is the thing being asserted.
    jq -n \
      --slurpfile audit $out/audit.json \
      --slurpfile allow $out/allowlist.json '
      ($allow[0] // {}) as $al
      | [ $audit[0][]
          | .protocol as $proto
          | (.field_comparisons // [])[]
          | select((.mismatches // []) | length > 0)
          | . as $fc
          | select(
              ( ($al[$proto] // [])
                | map(select(.offset_bits == $fc.offset_bits and .field == $fc.name))
                | length
              ) == 0
            )
          | {
              protocol: $proto,
              field: .name,
              offset_bits: .offset_bits,
              size_bits: .size_bits,
              mismatches: .mismatches
            }
        ]
    ' > $out/unallowlisted.json

    # The gated slice of the same set. Derived from unallowlisted.json rather
    # than recomputed, so the two can never disagree about what a delta is.
    jq --argjson gated '${gatedJSON}' \
      '[ .[] | select(.protocol as $p | $gated | index($p)) ]' \
      $out/unallowlisted.json > $out/unallowlisted-gated.json

    unallowlisted=$(jq 'length' $out/unallowlisted.json)
    gated_deltas=$(jq 'length' $out/unallowlisted-gated.json)
    missing_total=$(jq '[.[] | (.fields_missing // 0)] | add // 0' $out/audit.json)
    tcpinfo_missing=$(jq '[.[] | select(.protocol == "NL_Diag_TCPInfo") | (.fields_missing // 0)] | add // 0' $out/audit.json)

    echo
    echo "=== oracle result ==="
    echo "unallowlisted deltas        : $unallowlisted (advisory)"
    echo "  of which gated            : $gated_deltas (fails the build if non-zero)"
    echo "fields missing (all protos) : $missing_total"
    echo "fields missing NL_Diag_TCPInfo : $tcpinfo_missing"

    # NL_Diag_TCPInfo is called out by name because it is where the AccECN work
    # landed, and because its number is currently 11 for a reason that has
    # nothing to do with this repo: xdp2's registry asks the extractor for
    # TCPInfo6_10_3 rather than for the TCPInfo alias, so the AccECN trailer is
    # not in the struct proto-audit reads. 11 is therefore the EXPECTED value
    # until the xdp2 pin is bumped past a fix.
    #
    # Print it either way, and say which way it went, so neither number can be
    # mistaken for the other. A count above 11 is a real signal.
    if [ "$tcpinfo_missing" = "11" ]; then
      echo "      ^ expected: all 11 are the upstream registry pin, allowlisted"
      echo "        as kind=upstream-registry-pin. Not an xtcp2 gap; see"
      echo "        proto-audit-netlink-allowlist.json _note_NL_Diag_TCPInfo."
    elif [ "$tcpinfo_missing" != "0" ]; then
      echo "NOTE: NL_Diag_TCPInfo reports $tcpinfo_missing missing field(s)," >&2
      echo "      which is neither 0 (upstream fixed) nor 11 (upstream registry" >&2
      echo "      pin). Something has changed — read unallowlisted.json before" >&2
      echo "      touching the allowlist. See TODO-SOON.md §21." >&2
    fi

    if [ "$unallowlisted" != "0" ]; then
      echo
      echo "--- unallowlisted deltas ---"
      jq -r '.[] | "\(.protocol)  \(.field) @ bit \(.offset_bits) (\(.size_bits)b)"' \
        $out/unallowlisted.json
      echo
      echo "Each is either a real layout bug in pkg/xtcpnl or a deliberate"
      echo "choice that belongs in nix/checks/proto-audit-netlink-allowlist.json"
      echo "with a reason. Do not add an entry to quiet the check without"
      echo "establishing which of the two it is."
      echo "(advisory — none of these fails the build unless its protocol is gated)"
    fi

    # The gate. Deliberately a separate branch from the advisory print above:
    # the advisory count is expected to be large and to move as new protocols
    # are registered upstream, and nothing about that should be able to turn
    # this red. Only a delta on a protocol we have actually triaged does.
    if [ "$gated_deltas" != "0" ]; then
      echo
      echo "--- GATED deltas ---" >&2
      jq -r '.[] | "\(.protocol)  \(.field) @ bit \(.offset_bits) (\(.size_bits)b)"' \
        $out/unallowlisted-gated.json >&2
      echo >&2
      echo "FAIL: $gated_deltas unallowlisted delta(s) on gated protocol(s):" >&2
      echo "      ${gatedNames}" >&2
      echo "      These protocols are triaged, so a delta here is a real finding:" >&2
      echo "      either pkg/xtcpnl no longer matches the kernel layout, or a" >&2
      echo "      field moved offset and its allowlist entry stopped matching." >&2
      echo "      Read \$out/unallowlisted-gated.json before touching the allowlist." >&2
      exit 1
    fi
    # Empty when nothing is gated, so this says nothing in fully-advisory mode.
    if [ -n "${gatedNames}" ]; then
      echo "gated protocols clean: ${gatedNames}"
    fi

    echo
    echo "Full output: $out"
    ls -la $out/
  ''
