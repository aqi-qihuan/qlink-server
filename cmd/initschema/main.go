package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	mysqlHost := getEnv("MYSQL_HOST", "localhost")
	mysqlPort := getEnv("MYSQL_PORT", "3306")
	mysqlUser := getEnv("MYSQL_USER", "root")
	mysqlPwd := getEnv("MYSQL_PWD", "")

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/?charset=utf8mb4&parseTime=true&multiStatements=true",
		mysqlUser, mysqlPwd, mysqlHost, mysqlPort)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("connect failed: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("ping failed: %v", err)
	}
	fmt.Println("MySQL connected OK")

	sqlDir := filepath.Join(".", "sql")
	files := []string{
		"01_aqicloud_account.sql",
		"02_aqicloud_link_0.sql",
		"03_aqicloud_link_1.sql",
		"04_aqicloud_link_a.sql",
		"05_aqicloud_shop.sql",
		"30_ab_test.sql",
		"31_oidc_migration.sql",
		"32_p0_password.sql",
		"33_p2_migration.sql",
		"34_p3_migration.sql",
	}

	for _, f := range files {
		path := filepath.Join(sqlDir, f)
		content, err := os.ReadFile(path)
		if err != nil {
			log.Printf("SKIP %s: %v", f, err)
			continue
		}

		stmt := string(content)
		// Remove SET statements that Go driver doesn't handle well in multi-statement
		stmt = strings.ReplaceAll(stmt, "SET NAMES utf8mb4;", "")
		stmt = strings.ReplaceAll(stmt, "SET FOREIGN_KEY_CHECKS = 0;", "")
		stmt = strings.ReplaceAll(stmt, "SET FOREIGN_KEY_CHECKS = 1;", "")

		_, err = db.Exec(stmt)
		if err != nil {
			log.Printf("ERROR executing %s: %v", f, err)
			continue
		}
		fmt.Printf("OK: %s\n", f)
	}

	fmt.Println("Done")
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
