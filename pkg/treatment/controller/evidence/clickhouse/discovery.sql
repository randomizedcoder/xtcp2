-- Candidate discovery: destinations in one location that at least six hosts
-- talked to during the window, with the median of per-socket minimum RTT and
-- the pooled retransmission ratio, best traffic first. A row is a hypothesis
-- input; the controller still filters by template scope and the operator
-- still reviews every proposal.
WITH sockets AS (
 SELECT agent_id,host_boot_id,netns_inode,inet_diag_msg_socket_cookie,
 any(hex(inet_diag_msg_socket_destination)) AS destination_hex,
 any(inet_diag_msg_family) AS family,
 argMax(enrich_socket_dest_asn,timestamp_ns) AS asn,
 min(tcp_info_min_rtt)/1000 AS min_rtt_ms,
 max(tcp_info_bytes_sent)-min(tcp_info_bytes_sent) AS sent,
 max(tcp_info_bytes_retrans)-min(tcp_info_bytes_retrans) AS retrans
 FROM {{TABLE}}
 WHERE timestamp_ns>=fromUnixTimestamp64Nano({start:Int64}) AND timestamp_ns<fromUnixTimestamp64Nano({end:Int64})
 AND location={location:String} AND agent_id!='' AND inet_diag_msg_state=1
 GROUP BY agent_id,host_boot_id,netns_inode,inet_diag_msg_socket_cookie
 HAVING countDistinct(timestamp_ns)>=3
)
SELECT destination_hex,family,any(asn) AS asn,groupUniqArray(10000)(agent_id) AS agents,
 quantileExact(0.5)(min_rtt_ms) AS rtt_ms,sum(retrans)/greatest(1,sum(sent)) AS retransmission_ratio
FROM sockets GROUP BY destination_hex,family
HAVING length(agents)>=6 AND rtt_ms>={minimum_rtt:Float64} AND rtt_ms<={maximum_rtt:Float64}
 AND retransmission_ratio>={minimum_retransmission:Float64}
ORDER BY sum(sent) DESC LIMIT 10 FORMAT JSONEachRow
