package main

import (
	"fmt"
	"math/rand"
	"time"
)

func generateID() string {
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

func generateBolt11(amountMsat int64) string {
	return "lnbc" + formatMsat(amountMsat/1000) + "u1pjq" + randomString(40)
}

func formatMsat(msat int64) string {
	return fmt.Sprintf("%d", msat)
}
