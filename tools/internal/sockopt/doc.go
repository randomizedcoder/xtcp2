// Package sockopt holds the socket options tcp_client and tcp_server can
// set before connect or listen: the congestion algorithm, TOS (and the
// IPv6 traffic class), SO_PRIORITY, SO_RCVBUF and the IP_FREEBIND,
// IP_TRANSPARENT and IP_BIND_ADDRESS_NO_PORT bits. Test workloads use them
// to give the collector sockets with chosen inet_diag_cong, tos/tclass,
// class_id, window scales and inet_diag_sockopt.
package sockopt
