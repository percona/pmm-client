package utils

import (
	"math/rand"
	"time"
)

// GeneratePassword generate password.
func GeneratePassword(size int) string {
	rand.Seed(time.Now().UnixNano())
	required := []string{
		"abcdefghijklmnopqrstuvwxyz", "ABCDEFGHIJKLMNOPQRSTUVWXYZ", "0123456789", "_,;-",
	}
	var b []rune

	for _, source := range required {
		rsource := []rune(source)
		for i := 0; i < (size/len(required))+1; i++ {
			b = append(b, rsource[rand.Intn(len(rsource))])
		}
	}
	// Scramble.
	for range b {
		pos1 := rand.Intn(len(b))
		pos2 := rand.Intn(len(b))
		a := b[pos1]
		b[pos1] = b[pos2]
		b[pos2] = a
	}
	return string(b)[:size]
}
