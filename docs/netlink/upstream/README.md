# The iproute2 findings record

One directory per finding, one line per finding in the index below, and a
check that keeps both honest
([`test-goip-docs`](../../../nix/checks/goip-docs.nix): every artifact listed
here, every note linked from a reader, every link under `docs/netlink/`
resolved). The bar is set by the flagship,
[iproute2-bond-partner-state-width.md](iproute2-bond-partner-state-width.md):
found by a capture, reproduced, fixed, tested, reviewed — and ready to send.

## The index

| artifact | the finding | affected | fixed upstream | evidence | sent |
|---|---|---|---|---|---|
| [iproute2-bond-partner-state-width.md](iproute2-bond-partner-state-width.md) | `ip` reads the u16 `IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE` as a u8; big-endian hosts always print `0 <>` | ≤ HEAD | no (patches 0001–0003, this directory) | the LACP pair captures; `testsuite/tests/ip/link/show_bond_slave_lacp_state.t` | not yet; series ready |
| [iproute2-rta-addattr-uninit-padding.md](iproute2-rta-addattr-uninit-padding.md) | `rta_addattr_l` sends uninitialized stack bytes as attribute padding, so request bytes vary run to run | 7.2.0, 7.3.0 | yes — `c5b76cbb` (2026-11-09, untagged) | V038 (`cubic\0p\0` vs `cubic\0\0\0`); V069's 9 verdiff request diffs | nothing to send |
| [iproute2-print-json-bugs.md](iproute2-print-json-bugs.md) | `print_proto_down` reads the u32 reason value as a u8, gates it on `IFLA_BROADCAST` instead of the nest, and misses a space | ≤ HEAD | no | linkpd rows ec-004, pw-001, pw-006, rn1-004 (V065) | not yet; three lines |
| [iproute2-rt-names-table-load.md](iproute2-rt-names-table-load.md) | `rtnl_tab_initialize` accepts `id == size` (one pointer past the table); the name tables load lazily, so id 0 prints differently across one dump | ≤ HEAD | no | V062's 32 `dscp 0` readbacks; V066's realms | not yet; R1 is one character |
| [iproute2-devlink-print-widths.md](iproute2-devlink-print-widths.md) | `resource_show` prints three u64 fields through `print_uint`, and `pr_out_param_value_print` has no `U64` case | ≤ HEAD | no | the 22-row devlink ws corpus (genl-clones 1C/V079); both latent on netdevsim | not yet; drafts |
| [iproute2-vdpa-vstats-and-print-paths.md](iproute2-vdpa-vstats-and-print-paths.md) | `print_queue_type` mislabels rx and control queues without `MAX_VQP`, reads the u32 `QUEUE_INDEX` as u16, and emits JSON keys `"link "` and `"link_announce "` with trailing spaces; five more | ≤ HEAD | no | the `vdpa_sim_net` ws corpus (findings 1–4 reached) | not yet; drafts |
| [iproute2-ipfou-print-path.md](iproute2-ipfou-print-path.md) | `ip fou` can neither set nor show `FOU_ATTR_REMCSUM_NOPARTIAL`; the zero-check reads `af_bit_len(family)/8` bytes, not the attribute's length | ≤ HEAD | no | the 13-row fou ws corpus (genl-clones 3C/V085) | not yet; drafts |
| [iproute2-mptcp-show-paths.md](iproute2-mptcp-show-paths.md) | `ip -j mptcp limits show` prints text, not JSON; `limits show` accepts `subflows`/`add_addr_accepted` and silently reads the other; four more | ≤ HEAD | no | the 26-row mptcp ws corpus (findings 1, 2, 5, 6 demonstrated) | not yet; drafts |
| [iproute2-tipc-print-paths.md](iproute2-tipc-print-paths.md) | `tipc -j link get broadcast` dies of SIGSEGV, `tipc -h node get identity` calls a NULL help, `parse_attrs` writes `tb[type]` unbounded; thirteen more, two more crashes | ≤ HEAD | no | the 93-row tipc ws corpus (pw-022, exit 139) | not yet; drafts |
| [iproute2-bridge-print-paths.md](iproute2-bridge-print-paths.md) | `bridge fdb show` cannot filter a port name, a dump stops at the first port, four NULL reads, filters that leak between batch lines, and inconsistent exits and JSON — B1-B57 and K1-K9 | ≤ HEAD | no | the bridge ws corpus (6C/V097, 6X-C/V101, 7C/V108) and the `gobridge` unit rows | not yet; drafts |
| [iproute2-tunnel-tuntap.md](iproute2-tunnel-tuntap.md) | `ip tunnel show` overflows the stack on a long name, `ip tuntap` double-fcloses its file, text inside `-j`, an uninitialized byte, filters that match the wrong tunnels — T1-T25 | 7.2.0, ≤ HEAD | no | the 217-row tunnel corpus (8C/V002) and the `tnlsys` sidecars | not yet; drafts |
| [bug-classes.md](bug-classes.md) | not a finding report but the working checklist: the dozen recurring shapes the ~70 recorded bugs fall into, run over the target code before every conversion increment captures | — | — | every increment's plan since 6 (`bridge`) | not for sending |

The bond series' artifacts, all against `e11870d5`:

| artifact | what it is |
|---|---|
| [0000-cover-letter.patch](0000-cover-letter.patch) | Cover letter: issue → test → fix, LLM use disclosed |
| [0001-iplink_bond_slave-read-ad_partner_oper_port_state-as.patch](0001-iplink_bond_slave-read-ad_partner_oper_port_state-as.patch) | The one-line fix, with `Fixes:` |
| [0002-testsuite-fix-test_on_not-on-multi-line-output.patch](0002-testsuite-fix-test_on_not-on-multi-line-output.patch) | Fix the broken negative-match helper the test needs |
| [0003-testsuite-add-bond-slave-LACP-port-state-test.patch](0003-testsuite-add-bond-slave-LACP-port-state-test.patch) | Generator table + table-driven `.t` test |
| [second-opinion-review-2026-10-10.txt](second-opinion-review-2026-10-10.txt) | The review before sending |
| [test-results-2026-10-10.txt](test-results-2026-10-10.txt) | x86_64 and qemu-s390x results |
| [alternative-two-postings](alternative-two-postings) | The fix-alone-then-tests layout, kept as the fallback |

## When a note is owed

The rule, so the next capture does not decide this for itself:

- A capture, `verdiff` or replay difference that **both 7.2.0 boots agree
  on**, and that is **`ip`'s print path or request bytes** — not the
  kernel's, not goip's — obligates a note, in the same task, before goip is
  changed to match. A difference only one boot shows is a capture bug; fix
  the row.
- goip replicates `ip`'s behavior, faults included, behind the version
  knobs (the P14 doctrine, [goip-parity-next.md](../goip-parity-next.md)),
  and the note's "What it means for goip" section says so.
- An input the kernel cannot produce — a truncated attribute, an
  out-of-range width — gets a crafted-blob row
  ([goip-parity-events.md](../goip-parity-events.md), EP2) so the fault is
  exercised, not just described.
- Every iproute2 claim in a note is at `file:line` against a tag or hash
  that is pinned (`v7.2.0`, `v7.3.0`, or the scratch-clone HEAD the series
  is built on).
- A note goes upstream when the user decides, exactly as the bond series
  did; the index's "sent" column records the state. A finding already fixed
  upstream is still recorded — the note is how the corpus's tolerance for it
  is justified.
