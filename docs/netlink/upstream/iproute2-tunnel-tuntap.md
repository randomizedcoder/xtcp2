# iproute2 `ip tunnel` and `ip tuntap` bugs: a stack overflow, a double fclose, text inside JSON, an uninitialized byte, and filters that match the wrong tunnels

**Status (2026-11-23, increment 8):**

- **Not submitted to netdev.** These are drafts for the user's review, recorded here only, like the other write-ups in
  this directory. No patch, no PR, no sign-off.
- **Source:**
  - Line numbers are iproute2 **v7.2.0** (`ip/iptunnel.c`, `ip/ip6tunnel.c`, `ip/tunnel.c`, `ip/iptuntap.c`,
    `lib/utils.c`), the version the corpus was captured with.
  - "At HEAD" means the local `~/Downloads/iproute2` at `87cb3f41` (`v7.3.0-47`, 2026-11-18). For these five files
    that is the same as the GitHub mirror's `main` (checked with `gh api repos/iproute2/iproute2/commits?path=...`).
- **How they were found:** in increment 8 ([goip-tunnel.md](../goip-tunnel.md)).
  - The plan's bug-class sweep ([bug-classes.md](bug-classes.md)) listed leads T1–T17.
  - The captures (8C, V002) added T19–T22, and the clone (8R, V004) the rest.
  - `goip` replays all 195 replayable corpus rows byte for byte, so every captured finding below is reproduced by an
    independent implementation, not just read from the source.
- **Corpus:** `pkg/xtcpnl/testdata/7_3_1/ws/tunnel/` (171 rows) and `ws/tuntap/` (46), kernel 7.3.1. Each row has a
  `tnlsys.out` sidecar recording every ioctl `ip` made, sysfs read and `/proc` walk.
- **Reachability:**
  - In the corpus: T1, T5, T6, T7, T8, T9, T10, T11, T12, T13, T15, T16, T19, T20 and T22.
  - Latent, with unit rows in `internal/goip/obj_tunnel_test.go` / `obj_tuntap_test.go`: T2, T3, T4, T14, T17, T23,
    T24 and T25. They need a failure the kernel does not produce on request.
  - K1 and K2 are kernel or kernel-config notes.
  - Dropped: T18 (never written), T21, and the `show ""` lead.

## Contents

- [Summary](#summary)
- [T19. `ip tunnel show gre*` on an ip6gre device overflows `ip`'s stack (fixed after 7.3.0)](#t19-ip-tunnel-show-gre-on-an-ip6gre-device-overflows-ips-stack-fixed-after-730)
- [T14, T23, T24. `read_prop`: a double `fclose`, messages that are not silent, a missing newline](#t14-t23-t24-read_prop-a-double-fclose-messages-that-are-not-silent-a-missing-newline)
- [T20. `encaplimit` reads an uninitialized byte](#t20-encaplimit-reads-an-uninitialized-byte)
- [T1, T2. `-s` stats are bare `printf`, inside `-j`](#t1-t2--s-stats-are-bare-printf-inside--j)
- [T7, T9. JSON and newline inconsistencies between the paths](#t7-t9-json-and-newline-inconsistencies-between-the-paths)
- [T5, T6, T10, T12. What `show` actually lists](#t5-t6-t10-t12-what-show-actually-lists)
- [T8. A 16th isatap router is silently dropped](#t8-a-16th-isatap-router-is-silently-dropped)
- [T3. A refused `SIOCGETTUNNEL` in the list is a stderr line and exit 0](#t3-a-refused-siocgettunnel-in-the-list-is-a-stderr-line-and-exit-0)
- [T4, T11. Messages with a newline too many](#t4-t11-messages-with-a-newline-too-many)
- [T25. `tos 0x10/junk` drops the junk silently](#t25-tos-0x10junk-drops-the-junk-silently)
- [T13, T15, T16, T17, T22. `ip tuntap show`](#t13-t15-t16-t17-t22-ip-tuntap-show)
- [K1, K2. Kernel notes](#k1-k2-kernel-notes)
- [Dropped leads](#dropped-leads)

## Summary

| ID | File:line (v7.2.0) | Class | Finding | Pinned by | At HEAD | goip |
|---|---|---|---|---|---|---|
| T19 | iptunnel.c:415, 432 | C11 | `show NAME` with a `gre`/`ipip`/`sit`/`vti` prefix on an ip6gre device: the kernel writes a 76-byte `ip6_tnl_parm2` into a 52-byte stack struct | tunnel/ec-065 | fixed by `614e7481` (2026-10-20), not in v7.3.0 | prints the 52 bytes as `ip` does |
| T23 | utils.c:83, 100 | C8 | `read_prop` calls `fclose(fp)` twice on the not-a-number and ERANGE paths | unit | present | n/a (prints the messages) |
| T14 | utils.c:66-101 | C8 | a failed sysfs read is not silent: four messages, then `Failed to parse` | unit | present | same messages |
| T24 | utils.c:92-93 | C10 | the ERANGE message has no newline; `errno` is not cleared before `strtol` | unit | present | same text |
| T20 | ip6tunnel.c:184 | C9 | `get_u8(...) < -1` never fires; a bad `encaplimit` passes an uninitialized byte | tunnel/ec-063 | present (also link_ip6tnl.c:198) | uses 0, the corpus's value |
| T1 | tunnel.c:336-346, 410-412 | C5 | `-s` stats and the per-tunnel newline are bare `printf`/`fputc`, inside a `-j` document | tunnel/pw-010 | present | same bytes |
| T2 | tunnel.c:407-408 | C8 | a printed tunnel with no stats under `-s` ends the dump: "Dump terminated", exit 1 | unit | present | same |
| T7 | iptunnel.c:305-333, 344-366; ip6tunnel.c:81 | C7 | vti prints `ip/ip`; `ttl inherit`/`tos inherit` absent from JSON; repeated `pr`/`pdr` keys; remote `any` vs `0.0.0.0`; `dev` vs `link`; `"flags":[]` for gre only | tunnel/pw-003, pw-064 | present | same |
| T9 | ip6tunnel.c:363 | C5 | `ip -6 tunnel show NAME` prints no final newline; in a batch the next line runs on | tunnel/ec-044, ec-072 | present | same |
| T5 | iptunnel.c:252-264, 415-437 | C14 | `show mode X` or a `gre*`/`ipip*`/`sit*`/`isatap*`/`vti*` name takes the single-device path, not the list | tunnel/ec-022, ec-026, ng-001 | present | same |
| T6 | iptunnel.c:213-218 | C9 | `show key 5` is refused unless a gre or vti mode is given, and a mode skips the list the key would filter | tunnel/ng-002 | present | same |
| T10 | iptunnel.c:550-558 | C9 | the `mode` pre-scan: `ip -4 tunnel show mode any` is `ip -6 tunnel show` | tunnel/ec-036 | present | same |
| T12 | ip6tunnel.c:317-336 | C14 | `mode ip6ip6` lists vti6 devices and `mode vti6` lists ip6ip6 ones: the match ignores `VTI_ISVTI`; a proto-0 device matches every mode | tunnel/ec-049 | present | same |
| T8 | iptunnel.c:319-333 | C11 | `SIOCGETPRL` into a fixed 16-entry buffer, entry 0 the header: at most 15 routers print | tunnel/ec-038 | present | same |
| T3 | tunnel.c:397-398 | C8 | a refused `SIOCGETTUNNEL` in the list prints `get tunnel "X" failed` and skips the device, exit 0 | unit | present | same |
| T4 | tunnel.c:421; iptuntap.c:434 | C10 | `perror("Cannot send dump request\n")`: perror's `: strerror` lands on a line of its own | source | present | n/a |
| T11 | iptunnel.c:143 | C10 | `invarg("invalid TTL\n")`: invarg adds its own newline | tunnel/ng-006 | present (also link_gre.c:276, link_iptnl.c:220) | same |
| T25 | iptunnel.c:149-164 | C9 | `tos 0x10/junk` sets 0x10 and ignores `/junk` | unit | present | same |
| T13 | iptuntap.c:461 | C9 | `tuntap show` ignores every argument | tuntap/ec-013 | present | same |
| T15 | iptuntap.c:313-326 | C10 | `-d`: processes run together, `name(pid)name(pid)` | tuntap/ec-015 | present | same |
| T16 | iptuntap.c:420-426 | C5 | `-o -d`: `_SL_` before "Attached", a hard `\n` after the device | tuntap/ec-016 | present | same |
| T17 | iptuntap.c:368, 374-380 | C14 | only `ARPHRD_NONE`/`ETHER` pass, and `RTM_DELLINK` is accepted in a dump | unit | present | same |
| T22 | iptuntap.c:310-315 | C14 | `-d` matches a holder by device *name*: a process holding another namespace's `tap0` is listed under this one's | tuntap/ec-018 | present | same |

## T19. `ip tunnel show gre*` on an ip6gre device overflows `ip`'s stack (fixed after 7.3.0)

`do_show` (iptunnel.c:415-437) parses the arguments into a stack `struct ip_tunnel_parm p` (52 bytes). A name
starting `gre` sets `IPPROTO_GRE` (iptunnel.c:197), so `tnl_defname` returns `gre0` and the named path asks the
device directly: `tnl_get_ioctl(p.name, &p)`. The kernel answers `SIOCGETTUNNEL` from the *device's* driver. For an
ip6gre device that is `ip6gre_tunnel_siocdevprivate`, which copies a whole `struct ip6_tnl_parm2` (76 bytes) to
`ifr_data`. That writes 24 bytes past `p` on `ip`'s stack.

- **Row:** tunnel/ec-065, `ip tunnel show gre6x` (an ip6gre device named `gre6x`). The sidecar records the kernel's
  24-byte spill past the 52-byte argument (`tnlsys.out`, the only spill in the corpus). `ip` then prints the IPv6
  struct read as an IPv4 one: `gre6x: any/ip remote 32.1.13.184 local 0.0.0.1 ttl inherit tos inherit nopmtudisc
  ikey 0`. Here 32.1.13.184 is `2001:0db8`, and the GRE_KEY bit comes from the IPv6 flags word.
- **At HEAD:** `614e7481` ("iptunnel: use worst case buffer for tunnel ioctls", 2026-10-20) passes a
  `union { ip_tunnel_parm; ip6_tnl_parm2 }` on all three ioctl paths, so the overflow is gone. It is after v7.3.0, so
  no release has it yet.
- **Left after the fix:** the garbage line. `ip tunnel show gre6x` still prints an ip6gre tunnel decoded as a v4
  one, exit 0. The name-prefix heuristic picks the v4 struct for a device whose type it never checked. A fix would
  look up the device's ARPHRD (or `IFLA_INFO_KIND`), as the list path does, before choosing the struct.

## T14, T23, T24. `read_prop`: a double `fclose`, messages that are not silent, a missing newline

`ip tuntap show` reads `/sys/class/net/DEV/{tun_flags,owner,group}` with `read_prop` (lib/utils.c:52-102):

```c
	nl = strchr(buf, '\n');
	if (nl)
		*nl = '\0';

	fclose(fp);                                   /* :83 */
	result = strtol(buf, &endp, 0);

	if (*endp || buf == endp) {
		fprintf(stderr, "value \"%s\" in file %s is not a number\n", buf, fname);
		goto out;
	}

	if ((result == LONG_MAX || result == LONG_MIN) && errno == ERANGE) {
		fprintf(stderr, "strtol %s: %s", fname, strerror(errno));   /* :93, no \n */
		goto out;
	}
	...
out:
	fclose(fp);                                   /* :100 */
	fprintf(stderr, "Failed to parse %s\n", fname);
	return -1;
```

- **T23 (double fclose):** both the not-a-number and the ERANGE paths reach `out:` after the `fclose` at :83. Calling
  `fclose` on a closed `FILE *` is undefined behaviour. glibc usually tolerates it, but a stream freed by the first
  call can be reused by another `fopen` in between, or abort under `MALLOC_CHECK_`/ASan. The `fgets`-failed path
  jumps to `out:` before :83, so it is correct. **Fix:** move the `fclose` at :83 below the checks, or drop the
  `fclose` from `out:` and close before each `goto`.
- **T14 (not silent):** the plan's lead said a failed read drops the device silently. It does drop the device, but
  only after printing one of `fopen %s: %s`, `property "%s" in file %s is currently unknown`, `value "%s" in file %s
  is not a number` or the strtol line, and then `Failed to parse %s`. That is the intended behaviour; the lead is
  amended.
- **T24 (format):** the ERANGE message has no `\n`, so it and `Failed to parse` share one line. `errno` is never set
  to 0 before `strtol`. A stale `ERANGE` from an earlier call, together with a file that legitimately holds
  `LONG_MAX`, would be reported as out of range.
- **Rows:** none in the corpus: sysfs always has the three files with numbers in them. The unit rows in
  `TestGoipTuntapUnit` (fopen, not a number, empty, ERANGE) pin the texts goip reproduces. They cannot pin the double
  close.

## T20. `encaplimit` reads an uninitialized byte

```c
			} else {
				__u8 uval;

				if (get_u8(&uval, *argv, 0) < -1)          /* ip6tunnel.c:184 */
					invarg("invalid ELIM", *argv);
				p->encap_limit = uval;
```

`get_u8` returns 0 or -1, never less than -1, so the invarg never fires. On failure `get_u8` leaves `uval` unset,
and `ip` copies the uninitialized stack byte into the struct. In `show` that byte is a match filter: `ip -6 tunnel
show encaplimit 300` (tunnel/ec-063) listed every tunnel, so the byte happened to be 0. In `add` it is sent to the
kernel. Linux's `ip link ... type ip6tnl encaplimit` has the same test (link_ip6tnl.c:198). There goip refuses the
value as not implemented (goip-link-kinds, KG4-T05), because the request byte is unknowable. **Fix:** `if
(get_u8(&uval, *argv, 0))`.

## T1, T2. `-s` stats are bare `printf`, inside `-j`

`print_nlmsg_tunnel` (tunnel.c:400-414) prints each tunnel through `json_print`. Then, under `-s`, it calls
`tnl_print_stats`, which is five bare `printf`s (tunnel.c:336-346), and finally `fputc('\n', stdout)`. Under `-j` the
document becomes:

```
[{"ifname":"gre0",...,"flags":[]}
RX: Packets    Bytes        Errors CsumErrs OutOfSeq Mcasts
    0          0            0      0        0        0       
TX: Packets    Bytes        Errors DeadLoop NoRoute  NoBufs
    0          0            0      0        0        0     
,{"ifname":"ip_vti0",...}
```

(tunnel/pw-010.) That is not JSON. Even without `-s`, the bare newline gives `[{...}\n,{...}\n]`, which parses but
is not json_writer's output. The counters are `__u64` printed `%lld`.

**T2:** if `get_rtnl_link_stats_rta` finds no stats, the callback returns -1 after the tunnel was printed. That ends
the whole dump with "Dump terminated", exit 1. No kernel omits the stats from these dumps, so this is latent (unit
row).

## T7, T9. JSON and newline inconsistencies between the paths

- vti prints `ip/ip` (protocol IPIP), indistinguishable from ipip except by name.
- `ttl inherit` and `tos inherit` are `PRINT_FP`, so the JSON has no `ttl`/`tos` member for them. A consumer can't
  tell inherit from "not printed". v6's `hoplimit inherit` is the same.
- isatap routers are `"pr"` and `"pdr"` members, repeated within one object (`{"pr":"192.0.2.11","pdr":...}`). With
  two or more `pr`s the object has duplicate keys.
- `remote` is `any` in text but `"0.0.0.0"` in JSON (iptunnel.c:309); v6 always prints `::`.
- The link is `"dev"` in v4 JSON (iptunnel.c:344) and `"link"` in v6 (ip6tunnel.c:81), with the same text `dev X`.
- **T9:** v4's named path ends with `fputc('\n')` (iptunnel.c:437). v6's (ip6tunnel.c:363) does not, so `ip -6 tunnel
  show ip6t1` leaves the cursor on the line (tunnel/ec-044). In a batch the next command's output runs on:
  `...allow-localremotegre0: gre/ip remote any ...` (tunnel/ec-072). `-s NAME` takes the list path instead
  (ip6tunnel.c:348), so `-s` changes which code prints the tunnel (tunnel/ec-045).
- Both named paths print text under `-j`: they never call `new_json_obj`, so `print_*` finds no writer
  (tunnel/pw-022).

## T5, T6, T10, T12. What `show` actually lists

- **T5:** `tnl_defname` turns any mode, and any name with a `gre`/`ipip`/`sit`/`isatap`/`vti` prefix, into the
  single-device path. So `ip tunnel show mode gre` prints only `gre0`, the fallback device, with the parsed
  arguments as its lookup key (tunnel/ec-026), not every gre tunnel. `show gre9` is `get tunnel "gre9" failed: No
  such device`, exit 1 (tunnel/ng-001). The plan's "exit 255" was wrong: `do_show` returns -1 and `do_cmd` negates
  it.
- **T6:** `key`, `ikey` and `okey` are refused without a gre or vti mode ("Keys are not allowed with ipip and sit
  tunnels", exit 1, tunnel/ng-002), and with one the list is skipped. So `ip_tunnel_parm_match`'s `i_key` test is
  reachable only through a multicast `remote`, which sets the keys.
- **T10:** `do_iptunnel` scans its arguments for `mode` before anything else, and an IPv6 mode word sets
  `preferred_family = AF_INET6`, overriding `-4`. `ip tunnel show mode any` therefore lists the IPv6 tunnels
  (tunnel/ec-036), because `any` is in the IPv6 list (iptunnel.c:540).
- **T12:** `ip6_tnl_parm_match` compares `proto` only. `mode vti6` sets `IPPROTO_IPV6` plus `VTI_ISVTI` in `i_flags`,
  which the match never reads. So `ip -6 tunnel show mode ip6ip6` lists `ip6_vti0` (tunnel/ec-049), and `mode vti6`
  lists the ip6ip6 tunnels. The `!p2->proto` arm also passes a device configured `mode any` for every mode filter.

## T8. A 16th isatap router is silently dropped

`print_tunnel` passes `struct ip_tunnel_prl prl[16]`, entry 0 being the request header, so the kernel fills at most
15 entries. With 16 routers configured (`prl16` fixture), `ip tunnel show isa16` prints 15 and says nothing about
the 16th (tunnel/ec-038). The kernel reports the full count only if asked again with a bigger buffer, which `ip`
never does.

## T3. A refused `SIOCGETTUNNEL` in the list is a stderr line and exit 0

`print_nlmsg_tunnel` skips a device whose ioctl fails after `tnl_get_ioctl` prints `get tunnel "X" failed: %s`
(tunnel.c:397-398, :65). The listing then exits 0 with an incomplete list. No device in the corpus refuses (the
external and Ethernet kinds are filtered out by ARPHRD first), so this is a unit row.

## T4, T11. Messages with a newline too many

- **T4:** `perror("Cannot send dump request\n")` (tunnel.c:421, iptuntap.c:434) prints `Cannot send dump request`,
  then a newline, then `: <strerror>` on a line of its own. The same pattern is in bridge (B3's class).
- **T11:** `invarg("invalid TTL\n", *argv)` (iptunnel.c:143) gives `Error: argument "300" is wrong: invalid TTL`,
  then two newlines (tunnel/ng-006). The same text is in link_gre.c:276 and link_iptnl.c:220 at HEAD.

## T25. `tos 0x10/junk` drops the junk silently

```c
			dsfield = *argv;
			strsep(&dsfield, "/");
			if (strcmp(*argv, "inherit") != 0) {
				dsfield = *argv;
				p->iph.tos = 0;
			} else
				p->iph.tos = 1;
```

`strsep` cuts the word at the first `/`. For a value that is not `inherit`, the code then points `dsfield` back at
the cut word, so whatever followed the slash is never looked at: `tos 0x10/junk` is `tos 0x10`, and `tos 0x10/0x20`
too. Only `inherit/X` reads the part after the slash. A unit row (`TestTnlTOS`) and an ioctl-input row pin it.

## T13, T15, T16, T17, T22. `ip tuntap show`

- **T13:** `do_iptuntap` calls `do_show(argc-1, argv+1)`, and `do_show` never reads its arguments. `ip tuntap show
  dev tap0` lists every tun device (tuntap/ec-013), as does `show bogus`. The usage advertises `[ dev PHYS_DEV ]`.
- **T15:** under `-d`, each holder is `print_string("%s", name)` then `print_uint("(%d)", pid)` with no separator:
  `Attached to processes:python3(209813)python3(209816)` (tuntap/ec-015).
- **T16:** the line before is `print_string(PRINT_FP, NULL, "%s\tAttached to processes:", _SL_)`, but the device's
  line ends with a hard `print_string(PRINT_FP, NULL, "%s", "\n")`. So `-o -d` is one line per device with a
  backslash before "Attached" (tuntap/ec-016). Without `-d`, `-o` changes nothing.
- **T17:** `print_tuntap` accepts `RTM_DELLINK` as well as `RTM_NEWLINK` in a dump. It keeps only `ARPHRD_NONE` and
  `ARPHRD_ETHER`, which are the two types a tun device can have.
- **T22:** `show_processes` walks every `/proc/*/fd/*` and keeps descriptors whose fdinfo `iff:` equals the device
  *name*. `/proc` is shared across network namespaces, so a process holding a `tap0` in another namespace is listed
  under this namespace's `tap0` (tuntap/ec-018, fixture `xns`). The fdinfo carries no namespace or ifindex to tell
  them apart.

## K1, K2. Kernel notes

- **K1:** the workbench guest kernel has no `CONFIG_IPV6_SIT_6RD`. Every `SIOCGET6RD` is EINVAL, which `ip` skips
  silently (`tnl_ioctl_get_6rd`'s `skiperr`), and `ip tunnel 6rd` cannot be captured. The 6rd printer is covered by
  unit rows only.
- **K2:** an isatap tunnel made over netlink (`ip link add ... type sit mode isatap`) reports protocol 0 to
  `SIOCGETTUNNEL`, so `ip tunnel show` prints it as `any/ip` and never asks for its routers. One made with `ip tunnel
  add mode isatap` reports `IPPROTO_IPV6`. The `isan` device in the `tnl4` fixture pins the difference.

## Dropped leads

- **T21** ("`mode any` sticks across batch lines"): refuted. `ip_batch_cmd` resets `preferred_family` before each
  line, and tunnel/ec-071 shows line 2 back on v4. The nlcapgen comment that asserted it is corrected.
- **`ip tunnel show ""` is usage:** refuted. `matches("", "help")` returns non-zero for an empty prefix, so `""` falls
  through to `get_ifname`, which refuses it: `"name" not a valid ifname`, exit 255.
- **T18:** no such lead was written. The numbering jumps from the plan's T17 to 8C's T19.
