package main

import (
	"context"
	"fmt"
	"os"

	"github.com/redis/go-redis/v9"
)

func main() {
	rdb := redis.NewClient(&redis.Options{
		Addr:     "192.168.192.21:6379",
		Password: "aqi1015",
	})
	ctx := context.Background()

	if len(os.Args) < 2 {
		fmt.Println("usage: redis_get <key> [key2...]")
		os.Exit(1)
	}
	for _, key := range os.Args[1:] {
		val, err := rdb.Get(ctx, key).Result()
		if err != nil {
			fmt.Printf("%s => ERROR: %v\n", key, err)
		} else {
			fmt.Printf("%s => %s\n", key, val)
		}
	}
}
