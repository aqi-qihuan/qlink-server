-- OAuth SSO migration for qlink-server
-- Run against aqicloud_account database

USE `aqicloud_account`;

ALTER TABLE `account`
    ADD COLUMN `oidc_sub` VARCHAR(255) DEFAULT NULL COMMENT 'OAuth provider user ID',
    ADD COLUMN `oidc_provider` VARCHAR(255) DEFAULT NULL COMMENT 'OAuth provider name (google/github/wechat)',
    ADD INDEX `idx_oauth_sub_provider` (`oidc_sub`, `oidc_provider`);
