package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeDSN(t *testing.T) {
	uris := map[string]string{
		"mongodb://localhost:27017":                          "localhost:27017",
		"mongodb://admin:abc123@localhost:27017":             "admin:***@localhost:27017",
		"mongodb://admin:abc123@localhost":                   "admin:***@localhost",
		"mongodb://admin:abc123@localhost/database":          "admin:***@localhost/database",
		"mongodb://admin:abc123@localhost:27017/db?opt=true": "admin:***@localhost:27017/db",
		"admin:abc123@127.0.0.1:100":                         "admin:***@127.0.0.1:100",
		"localhost:27017/":                                   "localhost:27017",
		"localhost:27017?opt=5":                              "localhost:27017",
		"localhost":                                          "localhost",
		"admin:abc123@localhost:1,localhost:2":               "admin:***@localhost:1,localhost:2",
		"root:qwertyUIOP)(*&^%$#@1@localhost":                "root:***@localhost",
		"root:qwerty:UIOP)(*&^%$#@1@localhost":               "root:***@localhost",
	}

	for uri, expected := range uris {
		assert.Equal(t, expected, SanitizeDSN(uri), "uri = %s", uri)
	}
}
