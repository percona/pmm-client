package utils

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGeneratePassword(t *testing.T) {
	r, _ := regexp.Compile("^([[:alnum:]]|[_,;-]){20}$")
	r1, _ := regexp.Compile("[[:lower:]]")
	r2, _ := regexp.Compile("[[:upper:]]")
	r3, _ := regexp.Compile("[[:digit:]]")
	r4, _ := regexp.Compile("[_,;-]")

	assert.Len(t, GeneratePassword(5), 5)
	assert.Len(t, GeneratePassword(20), 20)
	assert.NotEqual(t, GeneratePassword(20), GeneratePassword(20))
	for i := 0; i < 10; i++ {
		p := GeneratePassword(20)
		c := r.Match([]byte(p)) && r1.Match([]byte(p)) && r2.Match([]byte(p)) && r3.Match([]byte(p)) && r4.Match([]byte(p))
		assert.True(t, c)
	}
}
