// where_code 在分库分表中定位指定短链 code 的实际落库位置。
// 用法: go run ./tools/where_code <code>
package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/go-sql-driver/mysql"
)

var dbs = []string{"aqicloud_link_0", "aqicloud_link_1", "aqicloud_link_a"}
var tables = []string{"short_link_0", "short_link_a"}

func main() {
	code := "a11MpqOa"
	if len(os.Args) > 1 {
		code = os.Args[1]
	}
	pwd := os.Getenv("MYSQL_PWD")
	host := os.Getenv("MYSQL_HOST")
	if pwd == "" || host == "" {
		fmt.Println("MYSQL_PWD and MYSQL_HOST env are required (read from .env, do not hardcode)")
		os.Exit(1)
	}
	found := false
	for _, db := range dbs {
		dsn := fmt.Sprintf("root:%s@tcp(%s:3306)/%s?charset=utf8mb4&parseTime=True", pwd, host, db)
		conn, err := sql.Open("mysql", dsn)
		if err != nil {
			fmt.Printf("%s: open error %v\n", db, err)
			continue
		}
		for _, tb := range tables {
			var n int
			q := fmt.Sprintf("SELECT count(*) FROM `%s`.`%s` WHERE code = ?", db, tb)
			if err := conn.QueryRow(q, code).Scan(&n); err != nil {
				fmt.Printf("%s.%s: query error %v\n", db, tb, err)
				continue
			}
			if n > 0 {
				found = true
				fmt.Printf(">>> FOUND: %s.%s rows=%d\n", db, tb, n)
			} else {
				fmt.Printf("%s.%s: 0\n", db, tb)
			}
		}
		conn.Close()
	}
	if !found {
		fmt.Printf("code %q NOT FOUND in any shard\n", code)
	}
}
