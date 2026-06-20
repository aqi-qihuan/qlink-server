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
	dsn := "root:aqi1015!@tcp(192.168.100.21:3307)/?charset=utf8mb4&parseTime=true&multiStatements=true"
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
		"aqicloud_account.sql",
		"aqicloud_link_0.sql",
		"aqicloud_link_1.sql",
		"aqicloud_link_a.sql",
		"aqicloud_shop.sql",
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
