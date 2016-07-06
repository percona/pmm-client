package pmm

import (
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestSanitizeURI(t *testing.T) {
	uris := map[string]string{
		"mongodb://localhost:27017": "localhost:27017",
		"mongodb://admin:abc123@localhost:27017": "admin:***@localhost:27017",
		"mongodb://admin:abc123@localhost": "admin:***@localhost",
		"mongodb://admin:abc123@localhost/database": "admin:***@localhost/database",
		"mongodb://admin:abc123@localhost:27017/db?opt=true": "admin:***@localhost:27017/db?opt=true",
		"admin:abc123@127.0.0.1:100": "admin:***@127.0.0.1:100",
		"localhost:27017": "localhost:27017",
		"localhost:27017?opt=5": "localhost:27017?opt=5",
		"localhost": "localhost",
		"admin:abc123@localhost:1,localhost:2": "admin:***@localhost:1,localhost:2",
	}

	convey.Convey("MongoDB uri sanitisation", t, func() {
		for uri, expect := range uris {
			convey.So(SanitizeURI(uri), convey.ShouldEqual, expect)
		}
	})
}
