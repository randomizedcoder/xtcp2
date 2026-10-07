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

The last three are a chain — the audit finds the gaps, the roadmap decides the
order, the status document records reality. Read them in that order.

## The parity harness

The decoders have a second consumer besides the daemon: `cmd/goip`, a read-only
reimplementation of `ip`'s `show` commands, and `cmd/goip-parity`, which drives
`ip` and `goip` against the same kernel and compares both the **netlink bytes
each sent** and the **text each printed**. It is the strongest evidence in the
tree that `pkg/xtcpnl` decodes what the kernel actually said, because the
reference is iproute2 itself rather than our reading of iproute2.

Twenty-nine commands are compared and twenty-four are gated. The counts, what
"gated" costs to earn, and the scope boundaries — read-only, five objects — are
in [parity at a glance](coverage-status.md#goip--ip-parity-at-a-glance). The
comparator is `pkg/nlparity` (protocol level) and `internal/goipparity`
(command level); the accepted-divergence list is
`pkg/nlparity/goip-parity-allowlist.json`, currently seven entries, all version
skew against the pinned `ip`, and its header comment is the doctrine for adding
to it.

```bash
nix run .#microvm-x86_64-goip-parity      # the live tier; needs /dev/kvm, not root
```

## Two constraints worth knowing up front

> **Read-only.** Decode messages, and build dump requests to solicit them.
> Never create, delete, or set. There is no write path in scope, now or later.

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
nix run .#microvm-x86_64-nlmon-capture    # events, hermetic microVM, no sudo
nix run .#capture-netlink-fixtures        # dumps (RTM_GET* pairs), host, needs sudo
```

Run both from the repo root. See
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
