# One inet_diag dump for every network namespace

An analysis of how `SOCK_DIAG_BY_FAMILY` dumps TCP sockets today, and where a dump covering every network namespace would go. It is written so it can become the body of a netdev RFC, but it proposes nothing on its own. Every reference is to Linux 7.3.0 at `46fec1c3029b` (2026-11-01).

## Objective

A privileged process should be able to get the TCP sockets of every network namespace from one `SOCK_DIAG_BY_FAMILY` dump, with each record tagged with its namespace.

Today a host-wide socket monitor has to do this for every namespace:

1. find it (scan `/proc/<pid>/ns/net`, or bind mounts under `/run/netns`);
2. `setns()` a locked thread into it and open a `NETLINK_SOCK_DIAG` socket there;
3. send a dump request on that socket every poll.

`ip -all netns exec ss` does the same, one namespace at a time. On a host with thousands of containers, that is thousands of sockets, threads entering namespaces, and dumps per poll. Visibility also depends on finding every namespace, which a tenant creating namespaces quickly can make harder.

## How a dump works today

### Every dump walks the shared table

`tcp_diag_dump()` (`net/ipv4/tcp_diag.c:314`) reads the table from the requester's namespace:

```c
hashinfo = net->ipv4.tcp_death_row.hashinfo;        /* tcp_diag.c:325 */
```

That is the global `tcp_hashinfo` for every namespace, unless the namespace was created while its parent had `net.ipv4.tcp_child_ehash_entries` set (`tcp_set_hashinfo()`, `net/ipv4/tcp_ipv4.c:3415-3432`; sysctl in `net/ipv4/sysctl_net_ipv4.c:1527`). Few hosts set it.

The dump makes three walks over the table, resumable across `recvmsg()` calls through `cb->args[0]` (phase), `cb->args[1]` (bucket) and `cb->args[2]` (position in bucket) (`tcp_diag.c:328-331`, `:557-558`):

| Phase | Table | Skips other namespaces' sockets at |
|---|---|---|
| 0 | listening hash `lhash2` | `tcp_diag.c:350` `net_eq(sock_net(sk), net)` |
| 1 | bound-inactive `bhash2` | `tcp_diag.c:416` `net_eq(ib2_net(tb2), net)` |
| 2 | established hash `ehash` (incl. TIME_WAIT, request sockets) | `tcp_diag.c:501` `net_eq(sock_net(sk), net)` |

Empty `ehash` buckets are skipped without taking the lock (`tcp_diag.c:488`). Every non-empty bucket is locked and its chain walked, and other namespaces' sockets are skipped one by one.

So **one namespace's dump costs O(sockets on the host)**, and polling N namespaces walks the shared table N times. A namespace holding a large socket table slows every other namespace's dump, even if those namespaces have no sockets at all.

### Filters run before a record is built

For each socket of the requester's namespace:

- the state filter (`tcp_diag.c:507`), where SYN_RECV implies NEW_SYN_RECV (`:326`);
- the family filter (`:509`);
- the bytecode filter, `inet_diag_bc_sk()` (`:520`, `net/ipv4/inet_diag.c:599`).

Bytecode is audited when the dump starts (`inet_diag_bc_audit()`, `inet_diag.c:728`). Only `INET_DIAG_BC_MARK_COND` needs `CAP_NET_ADMIN` (`inet_diag.c:769`; `net_admin = netlink_net_capable(skb, CAP_NET_ADMIN)` at `:742`).

### Dumps are paced by the reader

`netlink_recvmsg()` continues a running dump only once the socket's receive queue has drained to half of `sk_rcvbuf` (`net/netlink/af_netlink.c:2000`). The kernel builds the dump only as fast as userspace reads it.

### Other interfaces are per-namespace too

- The BPF `iter/tcp` iterator binds to the namespace of the task that creates it (`bpf_iter_init_seq_net()`, `fs/proc/proc_net.c:95`) and filters with `net_eq` (`seq_sk_match()`, `net/ipv4/tcp_ipv4.c:2478`).
- `/proc/net/sockstat` is per namespace for `inuse` and `tw` (`sockstat_seq_show()`, `net/ipv4/proc.c:51`).

## Where an all-namespace dump would go

### uapi

- A request attribute, after `INET_DIAG_REQ_PROTOCOL` in the request enum (`include/uapi/linux/inet_diag.h:65-69`): `INET_DIAG_REQ_ALL_NETNS` (flag).
- A record attribute, after `INET_DIAG_SOCKOPT` (`inet_diag.h:172`): `INET_DIAG_NETNS_COOKIE` (u64). The cookie is `net->net_cookie` (`include/net/net_namespace.h:178`, assigned once in `net/core/net_namespace.c:440`). Userspace already maps sockets to cookies with `SO_NETNS_COOKIE` (`include/uapi/asm-generic/socket.h:125`).

### Request parsing

`inet_diag_parse_attrs()` (`net/ipv4/inet_diag.c:181`, attribute checks at `:190`) fills `inet_diag_dump_data.req_nlas[]` (`include/linux/inet_diag.h:35`), so the flag is available to the handler as `cb_data->req_nlas[INET_DIAG_REQ_ALL_NETNS]` with no new plumbing. `__inet_diag_dump_start()` (`inet_diag.c:845`) is where to reject it.

### Permission

The dump exposes every tenant's sockets, so the request should be refused unless the requester:

- has `CAP_NET_ADMIN` in the initial user namespace (`capable()`, not `netlink_net_capable()`, which only checks the requester's own namespace, `inet_diag.c:742`); and
- sends it from a socket in `init_net`.

Check this in `__inet_diag_dump_start()`, so a refused request fails before any walk.

### The loop

In `tcp_diag_dump()` (`net/ipv4/tcp_diag.c:314`), with the flag set:

1. **Shared table.** Walk `tcp_hashinfo` as today, but don't apply the three namespace checks (`:350`, `:416`, `:501`). Every socket in the shared table is reported, whatever its namespace.
2. **Namespaces with their own table.** Then, under `rcu_read_lock()`, go over namespaces with `for_each_net_rcu()` (`include/net/net_namespace.h:447`) and, for each `net` where `net->ipv4.tcp_death_row.hashinfo != &tcp_hashinfo`, do the same three walks over that table.

Resuming across `recvmsg()` calls needs one more cursor: which namespace's table the walk is in. The `net` can't be held across calls without a reference. Cookies only grow, but the list isn't in cookie order: `setup_net()` takes the cookie (`net/core/net_namespace.c:440`), runs every pernet init, and only then appends the namespace under `net_rwsem` (`:446-448`). `pernet_ops_rwsem` is held for read there, so two namespaces being set up at once can be appended in either order. Two ways to resume:

- keep the cookie of the namespace being walked in `cb->args[3]` and, on the next call, find it again in the list (if it has gone, the walk can't tell which namespaces it already covered); or
- take a reference (`maybe_get_net()`) on the namespace being walked, kept in `cb->data`, and drop it when the dump moves on or ends.

### Record tagging

`inet_diag_msg_attrs_fill()` (`net/ipv4/inet_diag.c:97`) is the common attribute fill for full sockets; it would add `INET_DIAG_NETNS_COOKIE` from `sock_net(sk)->net_cookie`. TIME_WAIT and request sockets have their own fills, `tcp_twsk_diag_fill()` (`tcp_diag.c:192`) and `tcp_req_diag_fill()` (`tcp_diag.c:233`), and need it too, as does the reply size estimate for the new attribute.

## Open questions for netdev

1. **Cookie or inode.** The cookie is stable and already exposed (`SO_NETNS_COOKIE`). Tools key namespaces by `/proc/<pid>/ns/net` inode (`net->ns.inum`). Both, or one?
2. **inet_diag or BPF.** An `iter/tcp` variant that isn't bound to a namespace (`bpf_iter_init_seq_net()`, `seq_sk_match()`) might be the smaller change, but requires BPF where `SOCK_DIAG` doesn't.
3. **UDP, raw and the other diag handlers.** A flag that only TCP honours is surprising; should every inet handler check it, or reject it?
4. **Visibility.** Who should be allowed this, and should a namespace be able to opt out?
5. **Per-namespace tables.** Is the second loop needed, or should the flag only apply to the shared table (and be refused when any namespace has its own)?

## Precedents

- `IFLA_TARGET_NETNSID` (`include/uapi/linux/if_link.h:354`) lets rtnetlink act on another namespace, checked by `rtnl_get_net_ns_capable()` (`net/core/rtnetlink.c:2493`).
- `NETLINK_LISTEN_ALL_NSID` (`include/uapi/linux/netlink.h:172`) lets one socket receive notifications from every namespace with an nsid.
- `SO_NETNS_COOKIE` exposes a namespace's cookie to userspace.

## Before any RFC

- Search lore.kernel.org/netdev for earlier attempts at an all-namespace dump.
- Measure the shared-walk cost: a small namespace's `ss` time against the socket count in another namespace, with and without `tcp_child_ehash_entries` (the `microvm-x86_64-socket-scale` scenario).
- Re-check every reference here against the tree the RFC is based on.
