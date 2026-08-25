package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

func addressFromEnvironment() string {
	if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
		if _, err := strconv.Atoi(port); err == nil {
			return "127.0.0.1:" + port
		}
	}
	return defaultAddress
}

func validateAddress(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("-addr 格式无效：%w", err)
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return fmt.Errorf("监听地址必须是回环地址")
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1024 || value > 65535 {
		return fmt.Errorf("端口必须位于1024至65535")
	}
	return nil
}
