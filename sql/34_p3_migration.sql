-- P3 feature migrations
-- Run on aqicloud_link_0 (link database)

-- Abuse report table
CREATE TABLE IF NOT EXISTS `aqicloud_link_0`.`abuse_report` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `code` VARCHAR(16) NOT NULL COMMENT 'reported short link code',
  `reporter_account_no` BIGINT NOT NULL,
  `reason` VARCHAR(256) NOT NULL COMMENT 'report reason',
  `description` TEXT COMMENT 'detailed description',
  `status` VARCHAR(16) DEFAULT 'PENDING' COMMENT 'PENDING/RESOLVED/DISMISSED',
  `resolved_by` BIGINT DEFAULT NULL,
  `resolved_note` VARCHAR(512) DEFAULT '',
  `resolved_at` DATETIME DEFAULT NULL,
  `gmt_create` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_code` (`code`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='abuse reports for short links';

-- Brand configuration table
CREATE TABLE IF NOT EXISTS `aqicloud_link_0`.`brand_config` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `account_no` BIGINT NOT NULL,
  `site_name` VARCHAR(128) DEFAULT '',
  `logo_url` VARCHAR(512) DEFAULT '',
  `favicon_url` VARCHAR(512) DEFAULT '',
  `primary_color` VARCHAR(16) DEFAULT '#1677ff',
  `copyright` VARCHAR(256) DEFAULT '',
  `gmt_create` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `gmt_modified` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_account_no` (`account_no`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='brand customization config';
