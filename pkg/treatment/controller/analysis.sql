-- Scheduled whole-window query: an incremental MV cannot correctly compute
-- cross-insert counter deltas. Deduplicate poll timestamps before differences.
WITH sockets AS (
 SELECT agent_id, host_boot_id, netns_inode, inet_diag_msg_socket_cookie,
   min(timestamp_ns) AS first_at, max(timestamp_ns) AS last_at,
   countDistinct(timestamp_ns) AS samples,
   argMin(tcp_info_bytes_acked,timestamp_ns) AS first_acked,
   argMax(tcp_info_bytes_acked,timestamp_ns) AS last_acked,
   argMin(tcp_info_bytes_sent,timestamp_ns) AS first_sent,
   argMax(tcp_info_bytes_sent,timestamp_ns) AS last_sent,
   argMin(tcp_info_bytes_retrans,timestamp_ns) AS first_retrans,
   argMax(tcp_info_bytes_retrans,timestamp_ns) AS last_retrans,
   quantileExact(0.5)(tcp_info_rtt)/1000 AS socket_rtt,
   min(treatment_algorithm_exposed AND treatment_assignment_id={assignment:String}) AS exposed,
   arrayMax(arrayDifference(arraySort(groupUniqArray(20000)(toUnixTimestamp64Nano(timestamp_ns))))) AS maximum_gap_ns
 FROM {{TABLE}}
 WHERE timestamp_ns >= fromUnixTimestamp64Nano({start:Int64})
   AND timestamp_ns < fromUnixTimestamp64Nano({end:Int64})
   AND location={location:String} AND agent_id IN {agents:Array(String)}
   AND inet_diag_msg_state=1
   AND hex(inet_diag_msg_socket_destination)={destination_hex:String}
 GROUP BY agent_id,host_boot_id,netns_inode,inet_diag_msg_socket_cookie
)
SELECT agent_id,
 countIf(samples>=2) AS sockets,
 sum(if(last_acked>=first_acked,last_acked-first_acked,0)) AS bytes_acked,
 bytes_acked*8/(({end:Int64}-{start:Int64})/1e9) AS goodput_bps,
 sum(if(last_retrans>=first_retrans,last_retrans-first_retrans,0))/greatest(1,sum(if(last_sent>=first_sent,last_sent-first_sent,0))) AS retransmission_ratio,
 quantileExact(0.95)(socket_rtt) AS rtt_p95_ms,
 countIf(samples>=2 AND exposed) AS exposed_sockets,
 toBool(max(last_at)>=fromUnixTimestamp64Nano({end:Int64}-30000000000)
   AND max(maximum_gap_ns)<=30000000000
   AND countIf(last_acked<first_acked OR last_sent<first_sent OR last_retrans<first_retrans)=0) AS collection_healthy
FROM sockets
GROUP BY agent_id
ORDER BY agent_id
FORMAT JSONEachRow
