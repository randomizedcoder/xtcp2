
SELECT
    sec,
    hostname,
    inet_diag_msg_socket_source_port,
    inet_diag_msg_socket_destination_port
FROM
    xtcp.xtcp_records
ORDER BY sec DESC
LIMIT 20;

SELECT
    sec,
    nsec,
    hostname,
    inet_diag_msg_socket_source_port,
    inet_diag_msg_socket_destination_port,
    tcp_info_rtt,
    tcp_info_rtt_var,
    tcp_info_min_rtt,
    tcp_info_rcv_rtt,
    tcp_info_busy_time,
    tcp_info_rwnd_limited,
    tcp_info_sndbuf_limited,
    tcp_info_reordering,
    tcp_info_total_retrans,
FROM
    xtcp.xtcp_records
ORDER BY
    sec DESC,
    nsec DESC
LIMIT 20;