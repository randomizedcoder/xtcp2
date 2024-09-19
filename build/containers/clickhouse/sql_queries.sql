SELECT
    sec,
    nsec,
    hostname,
    tcp_info_rtt,
    tcp_info_rtt_var,
    tcp_info_min_rtt,
    tcp_info_rcv_rtt,
FROM xtcp.xtcp_records
ORDER BY
    sec DESC,
    nsec DESC
LIMIT 20;


SELECT COUNT(*) FROM xtcp.xtcp_records;
