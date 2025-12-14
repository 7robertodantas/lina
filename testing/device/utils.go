package device

import (
	"fmt"
	"math/rand"
	"time"
)

// GenerateID generates a unique ID based on timestamp and random string
func GenerateID() string {
	return time.Now().Format("20060102150405") + "-" + randomString(6)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// FormatMsat formats millisatoshis as a string
func FormatMsat(msat int64) string {
	return fmt.Sprintf("%d", msat)
}
