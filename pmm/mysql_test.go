package pmm

import (
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestGeneratePassword(t *testing.T) {
	convey.Convey("Password generation", t, func() {
		convey.So(generatePassword(6), convey.ShouldHaveLength, 6)
		convey.So(generatePassword(20), convey.ShouldHaveLength, 20)
	})
}
