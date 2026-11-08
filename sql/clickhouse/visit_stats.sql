-- ClickHouse visit_stats table
-- This file is NOT executed by MySQL docker-entrypoint.
-- Apply manually against ClickHouse HTTP port (8123):
--   curl -X POST "http://<ch-host>:8123/?user=default&password=<pwd>" \
--        --data-binary @clickhouse_visit_stats.sql

CREATE TABLE IF NOT EXISTS visit_stats.visit_stats
(
    `code`         String,
    `referer`      String,
    `is_new`       UInt64,
    `account_no`   UInt64,
    `province`     String,
    `city`         String,
    `ip`           String,
    `browser_name` String,
    `os`           String,
    `device_type`  String,
    `pv`           UInt64,
    `uv`           UInt64,
    `start_time`   DateTime,
    `end_time`     DateTime,
    `ts`           UInt64
)
ENGINE = ReplacingMergeTree(ts)
PARTITION BY toYYYYMMDD(start_time)
ORDER BY (start_time, end_time, code, province, city, referer, is_new, ip, browser_name, os, device_type)
SETTINGS index_granularity = 8192;
