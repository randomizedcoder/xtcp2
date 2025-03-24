--
-- Detach and query xtcp.xtcp_flat_records_kafka
--

DETACH TABLE xtcp.xtcp_flat_records_kafka;

SELECT
    *
FROM
    xtcp.xtcp_flat_records_kafka
SETTINGS
    stream_like_engine_allow_direct_select = 1;