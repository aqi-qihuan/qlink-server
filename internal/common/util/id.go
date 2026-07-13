package util

import (
	"fmt"
	"net"
	"strconv"

	"github.com/sony/sonyflake"
)

var sf *sonyflake.Sonyflake

func init() {
	// Match Java's SnowFlakeWordIdConfig: workerId = abs(ip.hashCode()) % 1024
	sf = sonyflake.NewSonyflake(sonyflake.Settings{
		MachineID: func() (uint16, error) {
			return getMachineID()
		},
	})
	if sf == nil {
		panic("sonyflake initialization failed: machine ID setup returned error")
	}
}

func getMachineID() (uint16, error) {
	ip := getLocalIP()
	// Java-style hashCode
	var h int32
	for _, c := range ip {
		h = 31*h + int32(c)
	}
	if h < 0 {
		h = -h
	}
	return uint16(h % 1024), nil
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}

// GenerateSnowflakeID returns a snowflake ID (int64).
// Compatible with Java's ShardingSphere SnowflakeShardingKeyGenerator.
// Panics on failure — returning 0 would cause primary key collisions and
// account number cross-contamination, which is far worse than a request failure.
func GenerateSnowflakeID() uint64 {
	if sf == nil {
		panic("sonyflake is not initialized: machine ID setup failed")
	}
	id, err := sf.NextID()
	if err != nil {
		panic(fmt.Sprintf("sonyflake NextID failed: %v", err))
	}
	return id
}

// GenerateSnowflakeIDStr returns snowflake ID as string.
func GenerateSnowflakeIDStr() string {
	return strconv.FormatUint(GenerateSnowflakeID(), 10)
}
