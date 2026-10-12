// Package rtnlmon is an rtnetlink multicast listener: it joins RTNLGRP_*
// groups, hands the caller a snapshot of the tables those groups cover, then
// every link, address, route and neighbor notification after it, in order.
//
// A caller that applies each Snapshot (replacing its state) and then each
// event (as an upsert or a delete) holds the kernel's current tables. That
// holds across a receive-buffer overrun too, which is the part of a listener
// that is easy to get wrong; Run's comment says how it is done.
package rtnlmon
