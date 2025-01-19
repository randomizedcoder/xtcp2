#!/bin/bash
# apparently /usr/bin/bash doesn't exist in the container

#
# This is the clickhouse database table creation script for xtcp2
#

set -e

if [ "$EUID" -ne 0 ]
then
	echo "Please run as root"
	exit 1
fi

rm -rf /docker-entrypoint-initdb.d/date
rm -rf /docker-entrypoint-initdb.d/date_utc
rm -rf /docker-entrypoint-initdb.d/whoami
rm -rf /docker-entrypoint-initdb.d/success
rm -rf /docker-entrypoint-initdb.d/xtcp.flat_xtcp_records_kafka*
rm -rf /docker-entrypoint-initdb.d/xtcp.flat_xtcp_records*
rm -rf /docker-entrypoint-initdb.d/xtcp.*

d=$(date +date%Y_%m_%d_%H_%M_%S)
du=$(date --utc +date%Y_%m_%d_%H_%M_%S)
w=$(whoami)

#usermod -d /home/das das

# https://clickhouse.com/docs/en/interfaces/formats#protobuf
# https://clickhouse.com/docs/en/interfaces/formats#protobufsingle
# The Containerfile now copies the .proto to the correct location
#cp /docker-entrypoint-initdb.d/flatxtcppb.proto /var/lib/clickhouse/format_schemas/flatxtcppb.proto

# https://protobuf.dev/programming-guides/encoding/#structure

echo "${d}" > /docker-entrypoint-initdb.d/date
echo "${du}" > /docker-entrypoint-initdb.d/date_utc
echo "${w}" > /docker-entrypoint-initdb.d/whoami

# TODO
#CODEC(T64, ZSTD(1))
#https://clickhouse.com/blog/optimize-clickhouse-codecs-compression-schema

# https://altinity.com/blog/2019-7-new-encodings-to-improve-clickhouse
# https://altinity.com/blog/clickhouse-for-time-series

clickhouse client -n <<-EOSQL
    SELECT now();

    --------------------------------------------------------------------------------------------------
    -- https://clickhouse.com/docs/en/cloud/bestpractices/asynchronous-inserts
    -- https://medium.com/@kn2414e/utilizing-go-and-clickhouse-for-large-scale-data-ingestion-and-application-146822f7020c
    -- FIX ME!!  Work out how to set this!!
    -- ALTER USER root SETTINGS async_insert = 1;

    --------------------------------------------------------------------------------------------------

    -- Reload protobufs
    -- https://clickhouse.com/docs/en/interfaces/formats#drop-protobuf-cache
    SYSTEM DROP FORMAT SCHEMA CACHE FOR Protobuf;

    --------------------------------------------------------------------------------------------------

    DROP DATABASE IF EXISTS xtcp;
    CREATE DATABASE IF NOT EXISTS xtcp;

    --------------------------------------------------------------------------------------------------

    DROP TABLE IF EXISTS xtcp.flat_xtcp_records;
    CREATE TABLE IF NOT EXISTS xtcp.flat_xtcp_records
    (
        -- https://clickhouse.com/docs/en/sql-reference/data-types/datetime64
        sec                                                         DateTime64(3,'UTC') CODEC(DoubleDelta, LZ4),
        nsec                                                        Int64,

        -- https://clickhouse.com/docs/en/sql-reference/data-types/lowcardinality
        hostname                                                    LowCardinality(String),

        netns                                                       String CODEC(ZSTD),
        nsid                                                        UInt32 CODEC(LZ4),

        label                                                       LowCardinality(String),
        tag                                                         LowCardinality(String),

        record_counter                                              UInt64 CODEC(DoubleDelta, LZ4),
        socket_fd                                                   UInt64 CODEC(LZ4),
        netlinker_id                                                UInt64 CODEC(LZ4),

        inet_diag_msg_family                                        UInt32 CODEC(LZ4),
        inet_diag_msg_state                                         UInt32 CODEC(LZ4),
        -- inet_diag_msg_family                                        LowCardinality(UInt32),
        -- inet_diag_msg_state                                         LowCardinality(UInt32),
        inet_diag_msg_timer                                         UInt32 CODEC(LZ4),
        inet_diag_msg_retrans                                       UInt32 CODEC(LZ4),

        inet_diag_msg_socket_source_port                            UInt32 CODEC(LZ4),
        inet_diag_msg_socket_destination_port                       UInt32 CODEC(LZ4),
        inet_diag_msg_socket_source                                 String CODEC(ZSTD),
        inet_diag_msg_socket_source_ipv4                            Nullable(IPv4),
        inet_diag_msg_socket_source_ipv6                            Nullable(IPv6),
        inet_diag_msg_socket_destination                            String CODEC(ZSTD),
        inet_diag_msg_socket_destination_ipv4                       Nullable(IPv4),
        inet_diag_msg_socket_destination_ipv6                       Nullable(IPv6),
        inet_diag_msg_socket_interface                              UInt32 CODEC(LZ4),
        inet_diag_msg_socket_cookie                                 UInt64 CODEC(LZ4),
        inet_diag_msg_socket_dest_asn                               UInt64 CODEC(LZ4),
        inet_diag_msg_socket_next_hop_asn                           UInt64 CODEC(LZ4),
        inet_diag_msg_socket_source_asn                             UInt64 CODEC(LZ4),

        inet_diag_msg_expires                                       UInt32 CODEC(LZ4),
        inet_diag_msg_rqueue                                        UInt32 CODEC(LZ4),
        inet_diag_msg_wqueue                                        UInt32 CODEC(LZ4),
        inet_diag_msg_uid                                           UInt32 CODEC(LZ4),
        inet_diag_msg_inode                                         UInt32 CODEC(LZ4),

        mem_info_rmem                                               UInt32 CODEC(LZ4),
        mem_info_wmem                                               UInt32 CODEC(LZ4),
        mem_info_fmem                                               UInt32 CODEC(LZ4),
        mem_info_tmem                                               UInt32 CODEC(LZ4),

        tcp_info_state                                              UInt32 CODEC(LZ4),
        tcp_info_ca_state                                           UInt32 CODEC(LZ4),
        -- tcp_info_state                                              LowCardinality(UInt32),
        -- tcp_info_ca_state                                           LowCardinality(UInt32),
        tcp_info_retransmits                                        UInt32 CODEC(LZ4),
        tcp_info_probes                                             UInt32 CODEC(LZ4),
        tcp_info_backoff                                            UInt32 CODEC(LZ4),
        tcp_info_options                                            UInt32 CODEC(LZ4),
        tcp_info_send_scale                                         UInt32 CODEC(LZ4),
        tcp_info_rcv_scale                                          UInt32 CODEC(LZ4),
        tcp_info_delivery_rate_app_limited                          UInt32 CODEC(LZ4),
        tcp_info_fast_open_client_failed                            UInt32 CODEC(LZ4),
        tcp_info_rto                                                UInt32 CODEC(LZ4),
        tcp_info_ato                                                UInt32 CODEC(LZ4),
        tcp_info_snd_mss                                            UInt32 CODEC(LZ4),
        tcp_info_rcv_mss                                            UInt32 CODEC(LZ4),
        tcp_info_unacked                                            UInt32 CODEC(LZ4),
        tcp_info_sacked                                             UInt32 CODEC(LZ4),
        tcp_info_lost                                               UInt32 CODEC(LZ4),
        tcp_info_retrans                                            UInt32 CODEC(LZ4),
        tcp_info_fackets                                            UInt32 CODEC(LZ4),
        tcp_info_last_data_sent                                     UInt32 CODEC(LZ4),
        tcp_info_last_ack_sent                                      UInt32 CODEC(LZ4),
        tcp_info_last_data_recv                                     UInt32 CODEC(LZ4),
        tcp_info_last_ack_recv                                      UInt32 CODEC(LZ4),
        tcp_info_pmtu                                               UInt32 CODEC(LZ4),
        -- tcp_info_pmtu                                               LowCardinality(UInt32),
        tcp_info_rcv_ssthresh                                       UInt32 CODEC(LZ4),
        tcp_info_rtt                                                UInt32 CODEC(LZ4),
        tcp_info_rtt_var                                            UInt32 CODEC(LZ4),
        tcp_info_snd_ssthresh                                       UInt32 CODEC(LZ4),
        tcp_info_snd_cwnd                                           UInt32 CODEC(LZ4),
        tcp_info_adv_mss                                            UInt32 CODEC(LZ4),
        tcp_info_reordering                                         UInt32 CODEC(LZ4),
        tcp_info_rcv_rtt                                            UInt32 CODEC(LZ4),
        tcp_info_rcv_space                                          UInt32 CODEC(LZ4),
        tcp_info_total_retrans                                      UInt32 CODEC(LZ4),
        tcp_info_pacing_rate                                        UInt64 CODEC(LZ4),
        tcp_info_max_pacing_rate                                    UInt64 CODEC(LZ4),
        tcp_info_bytes_acked                                        UInt64 CODEC(LZ4),
        tcp_info_bytes_received                                     UInt64 CODEC(LZ4),
        tcp_info_segs_out                                           UInt32 CODEC(LZ4),
        tcp_info_segs_in                                            UInt32 CODEC(LZ4),
        tcp_info_not_sent_bytes                                     UInt32 CODEC(LZ4),
        tcp_info_min_rtt                                            UInt32 CODEC(LZ4),
        tcp_info_data_segs_in                                       UInt32 CODEC(LZ4),
        tcp_info_data_segs_out                                      UInt32 CODEC(LZ4),
        tcp_info_delivery_rate                                      UInt64 CODEC(LZ4),
        tcp_info_busy_time                                          UInt64 CODEC(LZ4),
        tcp_info_rwnd_limited                                       UInt64 CODEC(LZ4),
        tcp_info_sndbuf_limited                                     UInt64 CODEC(LZ4),
        tcp_info_delivered                                          UInt32 CODEC(LZ4),
        tcp_info_delivered_ce                                       UInt32 CODEC(LZ4),
        tcp_info_bytes_sent                                         UInt64 CODEC(LZ4),
        tcp_info_bytes_retrans                                      UInt64 CODEC(LZ4),
        tcp_info_dsack_dups                                         UInt32 CODEC(LZ4),
        tcp_info_reord_seen                                         UInt32 CODEC(LZ4),
        tcp_info_rcv_ooopack                                        UInt32 CODEC(LZ4),
        tcp_info_snd_wnd                                            UInt32 CODEC(LZ4),

        congestion_algorithm_string                                 LowCardinality(String),
        congestion_algorithm_enum                                   LowCardinality(String),

        type_of_service                                             UInt32 CODEC(LZ4),
        traffic_class                                               UInt32 CODEC(LZ4),
        -- type_of_service                                             LowCardinality(UInt32),
        -- traffic_class                                               LowCardinality(UInt32),

        sk_mem_info_rmem_alloc                                      UInt32 CODEC(LZ4),
        sk_mem_info_rcv_buf                                         UInt32 CODEC(LZ4),
        sk_mem_info_wmem_alloc                                      UInt32 CODEC(LZ4),
        sk_mem_info_snd_buf                                         UInt32 CODEC(LZ4),
        sk_mem_info_fwd_alloc                                       UInt32 CODEC(LZ4),
        sk_mem_info_wmem_queued                                     UInt32 CODEC(LZ4),
        sk_mem_info_optmem                                          UInt32 CODEC(LZ4),
        sk_mem_info_backlog                                         UInt32 CODEC(LZ4),
        sk_mem_info_drops                                           UInt32 CODEC(LZ4),

        shutdown_state                                              UInt32 CODEC(LZ4),
        -- shutdown_state                                              LowCardinality(UInt32),

        vegas_info_enabled                                          UInt32 CODEC(LZ4),
        -- vegas_info_enabled                                          LowCardinality(UInt32),
        vegas_info_rtt_cnt                                          UInt32 CODEC(LZ4),
        vegas_info_rtt                                              UInt32 CODEC(LZ4),
        vegas_info_min_rtt                                          UInt32 CODEC(LZ4),

        dctcp_info_enabled                                          UInt32 CODEC(LZ4),
        -- dctcp_info_enabled                                          LowCardinality(UInt32),
        dctcp_info_ce_state                                         UInt32 CODEC(LZ4),
        dctcp_info_alpha                                            UInt32 CODEC(LZ4),
        dctcp_info_ab_ecn                                           UInt32 CODEC(LZ4),
        dctcp_info_ab_tot                                           UInt32 CODEC(LZ4),

        bbr_info_bw_lo                                              UInt32 CODEC(LZ4),
        bbr_info_bw_hi                                              UInt32 CODEC(LZ4),
        bbr_info_min_rtt                                            UInt32 CODEC(LZ4),
        bbr_info_pacing_gain                                        UInt32 CODEC(LZ4),
        bbr_info_cwnd_gain                                          UInt32 CODEC(LZ4),

        class_id                                                    UInt32 CODEC(LZ4), -- LowCardinality?
        sock_opt                                                    UInt32 CODEC(LZ4), -- LowCardinality?
        c_group                                                     UInt64 CODEC(LZ4),

    )
    ENGINE = MergeTree
    -- ENGINE = ReplicatedMergeTree
    -- Note that for xtcp repo, the docker is MergeTree, while k8s is ReplicatedMergeTree
    -- PARTITION BY toYYYYMMDD(sec)
    ORDER BY (sec, nsec, hostname, record_counter, netlinker_id, socket_fd)
    TTL toDateTime(sec) + INTERVAL 2 MONTH DELETE;

    --------------------------------------------------------------------------------------------------

    -- Not using Nested!
    -- https://clickhouse.com/docs/en/sql-reference/data-types/nested-data-structures/nested
    -- Not using Nullable, because it uses more space and apparently "almost always negatively affects performance"
    -- https://clickhouse.com/docs/en/sql-reference/data-types/nullable

    -- DROP TABLE xtcp.flat_xtcp_records_kafka;
    CREATE TABLE IF NOT EXISTS xtcp.flat_xtcp_records_kafka
    (
        -- https://clickhouse.com/docs/en/sql-reference/data-types/datetime64
        sec                                                         DateTime64(3,'UTC') CODEC(DoubleDelta, LZ4),
        nsec                                                        Int64,

        -- https://clickhouse.com/docs/en/sql-reference/data-types/lowcardinality
        hostname                                                    LowCardinality(String),

        netns                                                       String CODEC(ZSTD),
        nsid                                                        UInt32 CODEC(LZ4),

        label                                                       LowCardinality(String),
        tag                                                         LowCardinality(String),

        record_counter                                              UInt64 CODEC(DoubleDelta, LZ4),
        socket_fd                                                   UInt64 CODEC(LZ4),
        netlinker_id                                                UInt64 CODEC(LZ4),

        inet_diag_msg_family                                        UInt32 CODEC(LZ4),
        inet_diag_msg_state                                         UInt32 CODEC(LZ4),
        -- inet_diag_msg_family                                        LowCardinality(UInt32),
        -- inet_diag_msg_state                                         LowCardinality(UInt32),
        inet_diag_msg_timer                                         UInt32 CODEC(LZ4),
        inet_diag_msg_retrans                                       UInt32 CODEC(LZ4),

        inet_diag_msg_socket_source_port                            UInt32 CODEC(LZ4),
        inet_diag_msg_socket_destination_port                       UInt32 CODEC(LZ4),
        inet_diag_msg_socket_source                                 String CODEC(ZSTD),
        inet_diag_msg_socket_source_ipv4                            Nullable(IPv4),
        inet_diag_msg_socket_source_ipv6                            Nullable(IPv6),
        inet_diag_msg_socket_destination                            String CODEC(ZSTD),
        inet_diag_msg_socket_destination_ipv4                       Nullable(IPv4),
        inet_diag_msg_socket_destination_ipv6                       Nullable(IPv6),
        inet_diag_msg_socket_interface                              UInt32 CODEC(LZ4),
        inet_diag_msg_socket_cookie                                 UInt64 CODEC(LZ4),
        inet_diag_msg_socket_dest_asn                               UInt64 CODEC(LZ4),
        inet_diag_msg_socket_next_hop_asn                           UInt64 CODEC(LZ4),
        inet_diag_msg_socket_source_asn                             UInt64 CODEC(LZ4),

        inet_diag_msg_expires                                       UInt32 CODEC(LZ4),
        inet_diag_msg_rqueue                                        UInt32 CODEC(LZ4),
        inet_diag_msg_wqueue                                        UInt32 CODEC(LZ4),
        inet_diag_msg_uid                                           UInt32 CODEC(LZ4),
        inet_diag_msg_inode                                         UInt32 CODEC(LZ4),

        mem_info_rmem                                               UInt32 CODEC(LZ4),
        mem_info_wmem                                               UInt32 CODEC(LZ4),
        mem_info_fmem                                               UInt32 CODEC(LZ4),
        mem_info_tmem                                               UInt32 CODEC(LZ4),

        tcp_info_state                                              UInt32 CODEC(LZ4),
        tcp_info_ca_state                                           UInt32 CODEC(LZ4),
        -- tcp_info_state                                              LowCardinality(UInt32),
        -- tcp_info_ca_state                                           LowCardinality(UInt32),
        tcp_info_retransmits                                        UInt32 CODEC(LZ4),
        tcp_info_probes                                             UInt32 CODEC(LZ4),
        tcp_info_backoff                                            UInt32 CODEC(LZ4),
        tcp_info_options                                            UInt32 CODEC(LZ4),
        tcp_info_send_scale                                         UInt32 CODEC(LZ4),
        tcp_info_rcv_scale                                          UInt32 CODEC(LZ4),
        tcp_info_delivery_rate_app_limited                          UInt32 CODEC(LZ4),
        tcp_info_fast_open_client_failed                            UInt32 CODEC(LZ4),
        tcp_info_rto                                                UInt32 CODEC(LZ4),
        tcp_info_ato                                                UInt32 CODEC(LZ4),
        tcp_info_snd_mss                                            UInt32 CODEC(LZ4),
        tcp_info_rcv_mss                                            UInt32 CODEC(LZ4),
        tcp_info_unacked                                            UInt32 CODEC(LZ4),
        tcp_info_sacked                                             UInt32 CODEC(LZ4),
        tcp_info_lost                                               UInt32 CODEC(LZ4),
        tcp_info_retrans                                            UInt32 CODEC(LZ4),
        tcp_info_fackets                                            UInt32 CODEC(LZ4),
        tcp_info_last_data_sent                                     UInt32 CODEC(LZ4),
        tcp_info_last_ack_sent                                      UInt32 CODEC(LZ4),
        tcp_info_last_data_recv                                     UInt32 CODEC(LZ4),
        tcp_info_last_ack_recv                                      UInt32 CODEC(LZ4),
        tcp_info_pmtu                                               UInt32 CODEC(LZ4),
        -- tcp_info_pmtu                                               LowCardinality(UInt32),
        tcp_info_rcv_ssthresh                                       UInt32 CODEC(LZ4),
        tcp_info_rtt                                                UInt32 CODEC(LZ4),
        tcp_info_rtt_var                                            UInt32 CODEC(LZ4),
        tcp_info_snd_ssthresh                                       UInt32 CODEC(LZ4),
        tcp_info_snd_cwnd                                           UInt32 CODEC(LZ4),
        tcp_info_adv_mss                                            UInt32 CODEC(LZ4),
        tcp_info_reordering                                         UInt32 CODEC(LZ4),
        tcp_info_rcv_rtt                                            UInt32 CODEC(LZ4),
        tcp_info_rcv_space                                          UInt32 CODEC(LZ4),
        tcp_info_total_retrans                                      UInt32 CODEC(LZ4),
        tcp_info_pacing_rate                                        UInt64 CODEC(LZ4),
        tcp_info_max_pacing_rate                                    UInt64 CODEC(LZ4),
        tcp_info_bytes_acked                                        UInt64 CODEC(LZ4),
        tcp_info_bytes_received                                     UInt64 CODEC(LZ4),
        tcp_info_segs_out                                           UInt32 CODEC(LZ4),
        tcp_info_segs_in                                            UInt32 CODEC(LZ4),
        tcp_info_not_sent_bytes                                     UInt32 CODEC(LZ4),
        tcp_info_min_rtt                                            UInt32 CODEC(LZ4),
        tcp_info_data_segs_in                                       UInt32 CODEC(LZ4),
        tcp_info_data_segs_out                                      UInt32 CODEC(LZ4),
        tcp_info_delivery_rate                                      UInt64 CODEC(LZ4),
        tcp_info_busy_time                                          UInt64 CODEC(LZ4),
        tcp_info_rwnd_limited                                       UInt64 CODEC(LZ4),
        tcp_info_sndbuf_limited                                     UInt64 CODEC(LZ4),
        tcp_info_delivered                                          UInt32 CODEC(LZ4),
        tcp_info_delivered_ce                                       UInt32 CODEC(LZ4),
        tcp_info_bytes_sent                                         UInt64 CODEC(LZ4),
        tcp_info_bytes_retrans                                      UInt64 CODEC(LZ4),
        tcp_info_dsack_dups                                         UInt32 CODEC(LZ4),
        tcp_info_reord_seen                                         UInt32 CODEC(LZ4),
        tcp_info_rcv_ooopack                                        UInt32 CODEC(LZ4),
        tcp_info_snd_wnd                                            UInt32 CODEC(LZ4),

        congestion_algorithm_string                                 LowCardinality(String),
        congestion_algorithm_enum                                   LowCardinality(String),

        type_of_service                                             UInt32 CODEC(LZ4),
        traffic_class                                               UInt32 CODEC(LZ4),
        -- type_of_service                                             LowCardinality(UInt32),
        -- traffic_class                                               LowCardinality(UInt32),

        sk_mem_info_rmem_alloc                                      UInt32 CODEC(LZ4),
        sk_mem_info_rcv_buf                                         UInt32 CODEC(LZ4),
        sk_mem_info_wmem_alloc                                      UInt32 CODEC(LZ4),
        sk_mem_info_snd_buf                                         UInt32 CODEC(LZ4),
        sk_mem_info_fwd_alloc                                       UInt32 CODEC(LZ4),
        sk_mem_info_wmem_queued                                     UInt32 CODEC(LZ4),
        sk_mem_info_optmem                                          UInt32 CODEC(LZ4),
        sk_mem_info_backlog                                         UInt32 CODEC(LZ4),
        sk_mem_info_drops                                           UInt32 CODEC(LZ4),

        shutdown_state                                              UInt32 CODEC(LZ4),
        -- shutdown_state                                              LowCardinality(UInt32),

        vegas_info_enabled                                          UInt32 CODEC(LZ4),
        -- vegas_info_enabled                                          LowCardinality(UInt32),
        vegas_info_rtt_cnt                                          UInt32 CODEC(LZ4),
        vegas_info_rtt                                              UInt32 CODEC(LZ4),
        vegas_info_min_rtt                                          UInt32 CODEC(LZ4),

        dctcp_info_enabled                                          UInt32 CODEC(LZ4),
        -- dctcp_info_enabled                                          LowCardinality(UInt32),
        dctcp_info_ce_state                                         UInt32 CODEC(LZ4),
        dctcp_info_alpha                                            UInt32 CODEC(LZ4),
        dctcp_info_ab_ecn                                           UInt32 CODEC(LZ4),
        dctcp_info_ab_tot                                           UInt32 CODEC(LZ4),

        bbr_info_bw_lo                                              UInt32 CODEC(LZ4),
        bbr_info_bw_hi                                              UInt32 CODEC(LZ4),
        bbr_info_min_rtt                                            UInt32 CODEC(LZ4),
        bbr_info_pacing_gain                                        UInt32 CODEC(LZ4),
        bbr_info_cwnd_gain                                          UInt32 CODEC(LZ4),

        class_id                                                    UInt32 CODEC(LZ4), -- LowCardinality?
        sock_opt                                                    UInt32 CODEC(LZ4), -- LowCardinality?
        c_group                                                     UInt64 CODEC(LZ4),

    )
    ENGINE = Kafka
    SETTINGS
        kafka_broker_list = 'redpanda-0:9092',
        kafka_topic_list = 'xtcp',
        kafka_format = 'ProtobufList',
        -- kafka_format = 'Protobuf',
        kafka_schema = 'xtcp_flat_record.proto:XtcpFlatRecord',
        -- kafka_schema = 'xtcp_flat_record:xtcp_flat_record',
        -- kafka_schema = 'xtcp_flat_record.proto:Envelope',
        -- kafka_num_consumers = 1;
        -- kafka_thread_per_consumer = 0;
        kafka_group_name = 'xtcp';
    -- https://clickhouse.com/docs/en/interfaces/formats#protobuflist
    -- https://github.com/ClickHouse/ClickHouse/pull/35152
    -- kafka_format = 'ProtobufSingle',
    -- https://github.com/ClickHouse/ClickHouse/blob/master/src/Storages/Kafka/KafkaSettings.cpp

    --------------------------------------------------------------------------------------------------

    -- https://clickhouse.com/docs/en/integrations/kafka/kafka-table-engine#6-create-the-materialized-view
    DROP VIEW IF EXISTS xtcp.flat_xtcp_records_mv;

    CREATE MATERIALIZED VIEW IF NOT EXISTS xtcp.flat_xtcp_records_mv TO xtcp.flat_xtcp_records
    AS SELECT * FROM xtcp.flat_xtcp_records_kafka;

    -- DETACH TABLE xtcp.flat_xtcp_records_kafka;
    -- ATTACH TABLE xtcp.flat_xtcp_records_kafka;

    -- SELECT * FROM xtcp.flat_xtcp_records_kafka SETTINGS stream_like_engine_allow_direct_select = 1;
    -- DESCRIBE TABLE xtcp.flat_xtcp_records_kafka SETTINGS stream_like_engine_allow_direct_select = 1;

    -- https://clickhouse.com/docs/en/sql-reference/statements/describe-table
    DESCRIBE TABLE xtcp.flat_xtcp_records_kafka INTO OUTFILE '/docker-entrypoint-initdb.d/xtcp.flat_xtcp_records_kafka';
    DESCRIBE TABLE xtcp.flat_xtcp_records INTO OUTFILE '/docker-entrypoint-initdb.d/xtcp.flat_xtcp_records';

    DESCRIBE TABLE xtcp.flat_xtcp_records_kafka INTO OUTFILE '/docker-entrypoint-initdb.d/xtcp.flat_xtcp_records_kafka.csv' FORMAT CSV;
    DESCRIBE TABLE xtcp.flat_xtcp_records INTO OUTFILE '/docker-entrypoint-initdb.d/xtcp.flat_xtcp_records.csv' FORMAT CSV;

EOSQL

#-----------------------------------
# This code does a quick check that the tables for the Kafka and the real output
# table match.  The tables NEED to match.

if [ ! -f /usr/bin/sha512sum ]; then
    echo "/usr/bin/sha512sum not found";
    exit 1;
fi

file1='/docker-entrypoint-initdb.d/xtcp.flat_xtcp_records_kafka';
file2='/docker-entrypoint-initdb.d/xtcp.flat_xtcp_records';

sha512sum1=$(sha512sum $file1 | cut -d ' ' -f 1);
sha512sum2=$(sha512sum $file2 | cut -d ' ' -f 1);

if [ "${sha512sum1}" = "${sha512sum2}" ]; then
    echo "DESCRIBE TABLES MATCH.  Woot woot!";
else
    echo "DESCRIBE TABLES DO NOT MATCH!!  Fix the tables!!";
    exit 1;
fi


# https://github.com/ClickHouse/ClickHouse/blob/master/docker/server/README.md#how-to-extend-this-image

# https://github.com/ClickHouse/ClickHouse/blob/master/docker/server/entrypoint.sh

# https://stackoverflow.com/questions/75079434/clickhouse-protobuf-output-format

# https://clickhouse.com/docs/en/operations/server-configuration-parameters/settings#format_schema_path

# https://clickhouse.com/docs/en/engines/table-engines/integrations/kafka#configuration

# https://clickhouse.com/docs/en/engines/table-engines/integrations/nats
# CREATE TABLE [IF NOT EXISTS] [db.]table_name [ON CLUSTER cluster]
# (
#     name1 [type1] [DEFAULT|MATERIALIZED|ALIAS expr1],
#     name2 [type2] [DEFAULT|MATERIALIZED|ALIAS expr2],
#     ...
# ) ENGINE = NATS SETTINGS
#     nats_url = 'host:port',
#     nats_subjects = 'subject1,subject2,...',
#     nats_format = 'data_format'[,]
#     [nats_schema = '',]
#     [nats_num_consumers = N,]
#     [nats_queue_group = 'group_name',]
#     [nats_secure = false,]
#     [nats_max_reconnect = N,]
#     [nats_reconnect_wait = N,]
#     [nats_server_list = 'host1:port1,host2:port2,...',]
#     [nats_skip_broken_messages = N,]
#     [nats_max_block_size = N,]
#     [nats_flush_interval_ms = N,]
#     [nats_username = 'user',]
#     [nats_password = 'password',]
#     [nats_token = 'clickhouse',]
#     [nats_credential_file = '/var/nats_credentials',]
#     [nats_startup_connect_tries = '5']
#     [nats_max_rows_per_message = 1,]
#     [nats_handle_error_mode = 'default']

s=$(date +date%Y_%m_%d_%H_%M_%S)
echo "${s}" > /docker-entrypoint-initdb.d/success
echo "success:${s}"

#end