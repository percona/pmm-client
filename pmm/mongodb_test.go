package pmm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAdmin_DetectMongoDB(t *testing.T) {
	admin := Admin{}
	buildInfo, err := admin.DetectMongoDB("")
	assert.Nil(t, err)
	assert.NotEmpty(t, buildInfo)
}
