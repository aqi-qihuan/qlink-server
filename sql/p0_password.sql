-- P0 feature migration: password protection support
-- Run on all shard databases (aqicloud_link_0, aqicloud_link_1, aqicloud_link_a)

ALTER TABLE `short_link_0` ADD COLUMN `password` VARCHAR(255) DEFAULT '' COMMENT 'bcrypt hashed password' AFTER `state`;
ALTER TABLE `short_link_1` ADD COLUMN `password` VARCHAR(255) DEFAULT '' COMMENT 'bcrypt hashed password' AFTER `state`;
ALTER TABLE `short_link_a` ADD COLUMN `password` VARCHAR(255) DEFAULT '' COMMENT 'bcrypt hashed password' AFTER `state`;
