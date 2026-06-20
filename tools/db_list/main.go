package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	dsn := "root:aqi1015!@tcp(192.168.100.21:3306)/?charset=utf8mb4&parseTime=True"
	if len(os.Args) > 1 {
		dsn = os.Args[1]
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fmt.Println("connect error:", err)
		os.Exit(1)
	}
	defer db.Close()

	rows, err := db.Query("SHOW DATABASES")
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		rows.Scan(&name)
		fmt.Println(name)
	}
}
