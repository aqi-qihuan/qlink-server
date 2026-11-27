-- Idempotent product seed for the pricing page (scheme).
--
-- Background: sql/05_aqicloud_shop.sql drops+recreates `product` and inserts
-- 3 tiers, but the running DB had been overwritten with a single
-- "free-traffic" row (id=1), so the pricing page showed only the free tier.
--
-- Rules:
--   * id=1 MUST stay level=FIRST: account/service ClaimFree calls
--     fetchProduct(1) to issue the daily free traffic pack. Its business
--     fields (day_times / total_times / valid_day / plugin_type) are left at
--     their current values so already-issued packs and quota behavior are
--     unaffected; only display fields are corrected.
--   * title is rendered as-is by the Vue pricing page; use user-facing names.
--   * detail is split by "||" in Scheme.vue, with {{dayTimes}} interpolated.
--   * amount is the current price, old_amount the struck-through list price.
--
-- Safe to rerun: INSERT ... ON DUPLICATE KEY UPDATE, no DELETE / DROP.
--
-- IMPORTANT: apply with an explicit utf8mb4 client charset, otherwise the
-- mysql CLI defaults to latin1 and stores mojibake (e.g. "青铜" becomes
-- "é’é“”"):
--   docker exec -i shortlink-mysql mysql -uroot -p"$MYSQL_PWD" \
--     --default-character-set=utf8mb4 aqicloud_shop < sql/35_product_seed.sql

INSERT INTO `product`
  (`id`, `title`, `detail`, `img`, `level`, `old_amount`, `amount`, `plugin_type`,
   `day_times`, `total_times`, `valid_day`)
VALUES
  (1, '青铜会员-免费',
      '数据查看支持||日生成短链{{dayTimes}}次||限制跳转50次||默认域名',
      NULL, 'FIRST', 19, 0.00, 'DEFAULT', 100, 1000, 30),
  (2, '黄金会员-月度',
      '数据查看支持||日生成短链{{dayTimes}}次||跳转次数不限制||默认域名',
      NULL, 'SECOND', 99, 0.01, 'SHORT_LINK', 500, 10000, 30),
  (3, '黑金会员-月度',
      '数据查看支持||日生成短链{{dayTimes}}次||跳转次数不限制||自定义域名',
      NULL, 'THIRD', 199, 2.00, 'SHORT_LINK', 1000, 30000, 30)
ON DUPLICATE KEY UPDATE
  `title`      = VALUES(`title`),
  `detail`     = VALUES(`detail`),
  `img`        = VALUES(`img`),
  `level`      = VALUES(`level`),
  `old_amount` = VALUES(`old_amount`),
  `amount`     = VALUES(`amount`);
