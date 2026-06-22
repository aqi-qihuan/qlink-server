-- AB Test tables for qlink-server
-- These are stored in ds0 (aqicloud_link_0), non-sharded

CREATE TABLE IF NOT EXISTS `ab_test` (
    `id` BIGINT NOT NULL AUTO_INCREMENT COMMENT '实验ID',
    `account_no` BIGINT NOT NULL COMMENT '用户账号',
    `short_link_code` VARCHAR(32) NOT NULL COMMENT '短链码',
    `group_id` BIGINT DEFAULT 0 COMMENT '分组ID',
    `name` VARCHAR(255) NOT NULL COMMENT '实验名称',
    `description` VARCHAR(500) DEFAULT '' COMMENT '实验描述',
    `status` VARCHAR(20) NOT NULL DEFAULT 'draft' COMMENT 'draft/running/paused/completed',
    `traffic_split` VARCHAR(20) NOT NULL DEFAULT 'equal' COMMENT 'equal/weighted',
    `start_time` DATETIME NULL COMMENT '开始时间',
    `end_time` DATETIME NULL COMMENT '结束时间',
    `del` INT NOT NULL DEFAULT 0 COMMENT '软删除 0=正常 1=删除',
    `gmt_create` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `gmt_modified` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    INDEX `idx_acc_code` (`account_no`, `short_link_code`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='AB测试实验';

CREATE TABLE IF NOT EXISTS `ab_test_variant` (
    `id` BIGINT NOT NULL AUTO_INCREMENT COMMENT '变体ID',
    `ab_test_id` BIGINT NOT NULL COMMENT '实验ID',
    `name` VARCHAR(100) NOT NULL COMMENT '变体名称',
    `target_url` VARCHAR(2048) NOT NULL COMMENT '目标URL',
    `weight` INT NOT NULL DEFAULT 50 COMMENT '权重(百分比)',
    `is_control` INT NOT NULL DEFAULT 0 COMMENT '是否对照组 0=否 1=是',
    `description` VARCHAR(500) DEFAULT '' COMMENT '变体描述',
    `is_active` INT NOT NULL DEFAULT 1 COMMENT '是否激活 0=否 1=是',
    `gmt_create` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    INDEX `idx_ab_test_id` (`ab_test_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='AB测试变体';
