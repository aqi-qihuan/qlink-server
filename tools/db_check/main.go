package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	dsn := "root:aqi1015!@tcp(192.168.192.21:3307)/aqicloud_account?charset=utf8mb4&parseTime=True"
	if len(os.Args) > 1 {
		dsn = os.Args[1]
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fmt.Println("connect error:", err)
		os.Exit(1)
	}
	defer db.Close()

	// List all tables
	tables, err := db.Query("SHOW TABLES")
	if err != nil {
		fmt.Println("show tables error:", err)
	} else {
		fmt.Println("=== TABLES ===")
		for tables.Next() {
			var name string
			tables.Scan(&name)
			fmt.Println(" ", name)
		}
		tables.Close()
	}

	rows, err := db.Query("SELECT id, account_no, phone, username, secret, LEFT(pwd,40) as pwd_prefix FROM account ORDER BY id DESC LIMIT 5")
	if err != nil {
		fmt.Println("query error:", err)
		os.Exit(1)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var id, accountNo int64
		var phone, username, secret, pwdPrefix string
		rows.Scan(&id, &accountNo, &phone, &username, &secret, &pwdPrefix)
		fmt.Printf("id=%d account_no=%d phone=%s username=%s\nsecret=%s\npwd_prefix=%s\n", id, accountNo, phone, username, secret, pwdPrefix)
		found = true
	}
	if !found {
		fmt.Println("NO ACCOUNTS FOUND in account table (table may be empty)")
	}
}
