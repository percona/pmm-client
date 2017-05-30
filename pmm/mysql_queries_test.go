package pmm

import (
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestGetQuerySource(t *testing.T) {
	convey.Convey("getQuerySource", t, func() {
		querySource, _ := getQuerySource("testdata/qan-2b6c3eb3669943c160502874036968ba.conf")
		convey.So(querySource, convey.ShouldEqual, "perfschema")
	})
}

func TestGetQueryExamples(t *testing.T) {
	convey.Convey("getQueryExamples", t, func() {
		querySource, _ := getQueryExamples("testdata/qan-2b6c3eb3669943c160502874036968ba.conf")
		convey.So(querySource, convey.ShouldEqual, true)
	})
}
