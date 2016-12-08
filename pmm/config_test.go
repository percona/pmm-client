package pmm

import (
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestIsAddressLocal(t *testing.T) {
	ips := map[string]bool{
		"127.0.0.1":  true,
		"::1":        true,
		"8.8.8.8":    false,
		"127.0.0.2":  false,
		"127.0.0.11": false,
		"127.0.0.":   false,
		"":           false,
	}
	convey.Convey("Check if IP address is local", t, func() {
		for ip, expect := range ips {
			convey.So(isAddressLocal(ip), convey.ShouldEqual, expect)
		}
	})
}
