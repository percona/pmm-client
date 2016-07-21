package pmm

import (
	"regexp"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestGeneratePassword(t *testing.T) {
	r, _ := regexp.Compile("^([[:alnum:]]|[_,;-]){20}$")
	r1, _ := regexp.Compile("[[:lower:]]")
	r2, _ := regexp.Compile("[[:upper:]]")
	r3, _ := regexp.Compile("[[:digit:]]")
	r4, _ := regexp.Compile("[_,;-]")

	convey.Convey("Password generation", t, func() {
		convey.So(generatePassword(5), convey.ShouldHaveLength, 5)
		convey.So(generatePassword(20), convey.ShouldHaveLength, 20)
		convey.So(generatePassword(20), convey.ShouldNotEqual, generatePassword(20))
		for i := 0; i < 10; i++ {
			p := generatePassword(20)
			c := r.Match([]byte(p)) && r1.Match([]byte(p)) && r2.Match([]byte(p)) && r3.Match([]byte(p)) && r4.Match([]byte(p))
			convey.So(c, convey.ShouldBeTrue)
		}
	})
}
