package config

import (
	"fmt"
	"os"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// NewGormDB creates a new GORM database connection.
func NewGormDB(host string, port int, user, password, dbname string) (*gorm.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		user, password, host, port, dbname)

	// Use Warn level in production to avoid leaking SQL (including sensitive
	// data like password hashes, phone numbers) in logs. Info level prints
	// every query, which is useful for development but dangerous in production.
	logLevel := logger.Warn
	if os.Getenv("DB_LOG_LEVEL") == "info" {
		logLevel = logger.Info
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
	})
	if err != nil {
		return nil, fmt.Errorf("gorm connect failed: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)
	return db, nil
}
