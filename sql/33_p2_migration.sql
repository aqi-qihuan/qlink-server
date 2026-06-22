-- P2 feature migrations
-- Run on aqicloud_link_0 (link database)

-- API Token table (run on aqicloud_account)
CREATE TABLE IF NOT EXISTS `aqicloud_account`.`api_token` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `account_no` BIGINT NOT NULL,
  `name` VARCHAR(128) NOT NULL COMMENT 'token name/label',
  `token` VARCHAR(64) NOT NULL COMMENT 'hex token value',
  `scopes` VARCHAR(512) DEFAULT '' COMMENT 'comma-separated scopes',
  `expired_at` DATETIME NULL DEFAULT NULL,
  `last_used` DATETIME NULL DEFAULT NULL,
  `gmt_create` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_token` (`token`),
  KEY `idx_account_no` (`account_no`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='API access tokens';

-- Operation log table (run on aqicloud_link_0)
CREATE TABLE IF NOT EXISTS `aqicloud_link_0`.`operation_log` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `account_no` BIGINT NOT NULL,
  `action` VARCHAR(64) NOT NULL COMMENT 'e.g. link:create, domain:delete',
  `resource` VARCHAR(128) DEFAULT '' COMMENT 'resource type',
  `resource_id` VARCHAR(128) DEFAULT '' COMMENT 'resource identifier',
  `ip` VARCHAR(64) DEFAULT '',
  `user_agent` VARCHAR(512) DEFAULT '',
  `details` TEXT COMMENT 'JSON details',
  `gmt_create` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_account_no` (`account_no`),
  KEY `idx_action` (`action`),
  KEY `idx_gmt_create` (`gmt_create`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='operation audit log';

-- Domain table migration: add state column (run on aqicloud_link_0)
ALTER TABLE `aqicloud_link_0`.`domain` ADD COLUMN `state` VARCHAR(16) DEFAULT 'ACTIVE' COMMENT 'ACTIVE/INACTIVE' AFTER `value`;
