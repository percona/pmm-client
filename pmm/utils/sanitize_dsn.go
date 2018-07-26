package utils

import (
	"fmt"
	"strings"
)

// SanitizeDSN removes password from DSN.
func SanitizeDSN(dsn string) string {
	dsn = strings.TrimRight(strings.Split(dsn, "?")[0], "/")
	dsn = strings.TrimPrefix(dsn, "mongodb://")
	if strings.Index(dsn, "@") > 0 {
		dsnParts := strings.Split(dsn, "@")
		userPart := dsnParts[0]
		hostPart := ""
		if len(dsnParts) > 1 {
			hostPart = dsnParts[len(dsnParts)-1]
		}
		userPasswordParts := strings.Split(userPart, ":")
		dsn = fmt.Sprintf("%s:***@%s", userPasswordParts[0], hostPart)
	}
	return dsn
}
