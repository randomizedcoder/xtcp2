# Netlink documentation

Everything about how xtcp2 talks to the kernel over netlink: the production
`inet_diag` read path, how its coverage compares to the reference Go netlink
library, and the roadmap for broadening that coverage.

If you are looking for the daemon as a whole, start at the
[documentation hub](../README.md).

## The documents

| Document | Read it for |
|---|---|
| [Netlink TCP collection](collection.md) | **Start here.** The production path: `inet_diag` dump requests, the 13 attribute deserializers, the `.pcap` fixture corpus and how to regenerate it. |
| [Non-blocking netlink](nonblocking.md) | The design for non-blocking socket reads — why the blocking `recvmsg` path bounds OS-thread scaling, and what replacing it involves. |
| [Netlink parsing comparison](parsing-comparison.md) | The audit: what `pkg/xtcpnl` parses versus `vishvananda/netlink`, message-type and attribute coverage on both sides, the two test strategies, and the prioritised gaps. |
| [Netlink coverage expansion](coverage-expansion.md) | The roadmap acting on that audit: the read-only constraint, the target subpackage layout, generalising the `nlmon` capture harness to every protocol family, and the eight phases. |
| [Netlink coverage status](coverage-status.md) | The live tracker: what has actually landed, the measured baseline, each phase's exit criteria, and the **goip ↔ `ip` parity** state — start at [parity at a glance](coverage-status.md#goip--ip-parity-at-a-glance). |
| [Route write path](route-write-path.md) | The design for goip's first write capability: generating `ip route add/change/replace/del` requests with per-route TCP tuning, to byte-parity with iproute2. Proposed; narrows the read-only invariant below. |
| [Link write path](link-write-path.md) | The second write design, extending the route one: `ip link add/set/del` with a focus on bonding and LACP (802.3ad) — the bond `IFLA_INFO_DATA` encoder and decoder, and the two-namespace and two-microVM capture topologies in which LACP actually negotiates, so fixtures carry real partner state. Phases 0 to 8 landed: `goip link add/set/del` builds `ip`'s request byte for byte, as a dry run, the 21 write rows are gated in `goip-parity`, and the bond decode is green unchanged on a second corpus captured on virtio-net ports with the LACP partner in another VM. |
| [Address write path](addr-write-path.md) | The third write design, completing `ip address`: the show selectors (`to`, `scope`, flag filters, `label`, `master`/`vrf`/`nomaster`, `type`, `proto`, …), `-br` and `-o`, the four unread `IFA_*` attributes, and dry-run `addr add/change/replace/delete`, `flush`, `save`, `showdump` and `restore` to byte-parity with iproute2 — with the per-row address topologies (secondary, peer, label, lifetimes, tentative, dadfailed, stable-privacy) that give each a captured fixture. Every phase has landed ([status](addr-write-path-status.md)): the selectors, `-br`/`-o`, the writes, `flush` and `restore` are gated `goip-parity` rows, except `addr show dynamic`, whose lifetime ticks; `save` and `showdump` are checked offline against `ip`'s bytes, `showdump` because it sends no netlink. |
| [Address write path — implementation plan](addr-write-path-plan.md) | The address design's phases as 25 gated tasks (`A0-T01` … `A9-T03`): dependencies, files to deliver, the design test table each task owns, and the exact command that closes it. |
| [Address write path — status](addr-write-path-status.md) | The tracker for that plan: phase and task states, the four Phase 0 measurements, which task amends each flipped statement, and the verification ledger. |
| [goip parity: remaining gaps](goip-parity-gaps.md) | What full `ip` coverage still needs: the 20 objects goip does not run, the missing verbs on the 11 it does, the generic netlink and XFRM families, and the 23 ungated rows, 22 of which G0 has gated. For each netlink message type `ip` uses, a measured count of nlmon pcaps, table-driven parse tests and parse benchmarks. The plan to close the gaps runs in three tiers: complete the capture corpus, then the parsers, then goip and the matrix. |
| [goip parity gaps — status](goip-parity-gaps-status.md) | The tracker for that plan: the next action, the state of each phase from C0 to G7, the task checklist, the verification ledger and the decision log. A phase is closed, and the assessment re-measured, in its last task PR. |
| [Upstream bug: bond partner state width](upstream/iproute2-bond-partner-state-width.md) | An iproute2 bug found while designing the link write path: `ip` reads the u16 `IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE` as a u8, so big-endian hosts always print `0 <>`. |

The last three are a chain — the audit finds the gaps, the roadmap decides the
order, the status document records reality. Read them in that order.

## The parity harness

The decoders have a second consumer besides the daemon: `cmd/goip`, a read-only
reimplementation of `ip`'s `show` commands, and `cmd/goip-parity`, which drives
`ip` and `goip` against the same kernel and compares both the **netlink bytes
each sent** and the **text each printed**. It is the strongest evidence in the
tree that `pkg/xtcpnl` decodes what the kernel actually said, because the
reference is iproute2 itself rather than our reading of iproute2.

281 commands are compared, 133 of them dry-run link, link property, route,
address, neighbor, rule, nexthop, address label, neighbor table, statistics,
token and nsid writes, flushes and restores, and 272 are gated. The counts, what "gated" costs to earn, and the scope boundaries are
in [parity at a glance](coverage-status.md#goip--ip-parity-at-a-glance). The
comparator is `pkg/nlparity` (protocol level) and `internal/goipparity`
(command level); the accepted-divergence list is
`pkg/nlparity/goip-parity-allowlist.json`, currently twelve entries, seven version
skew against the pinned `ip` and five the `ip netns` rows' `NETNSA_FD`, a
descriptor number no two processes share, and its header comment is the doctrine for adding
to it.

```bash
nix run .#microvm-x86_64-goip-parity      # the live tier; needs /dev/kvm, not root
```

## Two constraints worth knowing up front

> **Read-only.** Decode messages, and build dump requests to solicit them.
> Never create, delete, or set. There is no write path in scope, now or later.

**Narrowed.** Write paths for `ip route` and `ip link` are designed — see
[route-write-path.md](route-write-path.md) and
[link-write-path.md](link-write-path.md) — and they narrow this invariant
rather than delete it. Link-write Phase 4 landed the gate: the read path stays
`RTM_GET*`-only and test-enforced through `BuildRequest`, and a separate
`BuildWriteRequest` admits exactly `RTM_NEWROUTE`, `RTM_DELROUTE`,
`RTM_NEWLINK`, `RTM_DELLINK`, and, since the address design's Phase 4,
`RTM_NEWADDR` and `RTM_DELADDR`, and, since
[goip-parity-gaps.md](goip-parity-gaps.md)'s G3-T01, `RTM_NEWNEIGH` and
`RTM_DELNEIGH`, and, since its G3-T02, `RTM_NEWRULE` and `RTM_DELRULE`, and,
since its G3-T03, `RTM_NEWNEXTHOP` and `RTM_DELNEXTHOP`, and, since its G3-T04,
`RTM_NEWADDRLABEL` and `RTM_DELADDRLABEL`, and, since its G3-T05,
`RTM_SETNEIGHTBL` and `RTM_SETSTATS`, and, since its G3-T06,
`RTM_NEWLINKPROP` and `RTM_DELLINKPROP`. Requests built there are written to a file for
comparison and never sent; no automated path applies a write to a live kernel.
Both dry runs have since landed on that gate: `goip link
add|set|change|replace|delete` and `goip route
add|change|replace|prepend|append|test|delete` build `ip`'s request
byte for byte and write it to `GOIP_WRITE_OUT`, as `goip neigh
add|change|replace|delete|flush`, `goip rule add|delete|flush|restore`,
`goip nexthop add|replace|delete|flush`, `goip addrlabel add|delete|flush`,
`goip ntable change`, `goip stats set` and `goip link property add|del`
now do too, and refuse with
`ErrWriteNotApplied` when the hook is unset.

This is what makes a twenty-family surface finite, and it is the main reason
coverage numbers here are not comparable with `vishvananda/netlink` — that
library exists to *configure* the stack, this one exists to *read* it.

> **Test fixtures are real captures.** Positive fixtures are real `nlmon`
> captures of real kernel bytes, committed under
> `pkg/xtcpnl/testdata/<kernel>/`. Hand-assembled bytes are for truncation and
> malformed-input rows only.

A synthetic fixture encodes the author's belief about the layout, so the decoder
and its test can be wrong together and still pass. Generate fixtures with:

```bash
nix run .#microvm-x86_64-nlmon-capture         # events, hermetic microVM, no sudo
nix run .#microvm-x86_64-netlink-dump-capture  # dumps (RTM_GET* pairs), microVM, no sudo
nix run .#microvm-x86_64-link-write-capture    # write rows and readbacks, microVM, no sudo
```

Run them from the repo root, and pass `--out DIR` first: a run rewrites every
fixture it captures. `nix run .#capture-netlink-fixtures` captures the same
dumps on the host, but needs sudo and picks up other processes' traffic, so
`flake.nix` keeps it as a diagnostic fallback only. See
[collection.md](collection.md#regenerating-the-fixtures) for the mechanics and
[coverage-expansion.md](coverage-expansion.md#fixture-provenance-real-captures-not-hand-assembled-bytes)
for why it is a gate rather than a preference.

## Related, outside this directory

- [Integration testing](../integration-testing.md) — the QEMU microVM harness,
  including the `nlmon-capture` fixture generator.
- [Testing & quality](../testing-and-quality.md) — the multi-kernel fixture
  corpus and the custom audit tools.
- [Locality enrichment](../locality-enrichment.md) — the main consumer of the
  rtnetlink link/addr/route decoders.
- [Network namespaces](../network-namespaces.md) — why there is one netlink
  socket per namespace.
