package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

type DebitRequest struct {
	ID          string  `json:"ID"`
	DeviceID    string  `json:"DeviceID"`
	WindowStart int64   `json:"WindowStart"`
	WindowEnd   int64   `json:"WindowEnd"`
	Units       float64 `json:"Units"`
	Unit        string  `json:"Unit"`
	UnitPrice   float64 `json:"UnitPrice"`
	TotalSats   float64 `json:"TotalSats"`
	CreatedAt   int64   `json:"CreatedAt"`
}

type LedgerEntry struct {
	DebitRequest
	ProcessedAt int64  `json:"ProcessedAt"`
	Status      string `json:"Status"`
}

var ledger = make(map[string]LedgerEntry)

func postDebit(c *gin.Context) {
	var req DebitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	idempotencyKey := c.GetHeader("Idempotency-Key")
	if idempotencyKey == "" {
		idempotencyKey = req.ID
	}
	if entry, exists := ledger[idempotencyKey]; exists {
		c.JSON(http.StatusOK, entry)
		return
	}
	entry := LedgerEntry{
		DebitRequest: req,
		ProcessedAt: time.Now().Unix(),
		Status:      "debited",
	}
	ledger[idempotencyKey] = entry
	log.Printf("[ledger] debited: %+v", entry)
	c.JSON(http.StatusOK, entry)
}

func getLedger(c *gin.Context) {
	var out []LedgerEntry
	for _, entry := range ledger {
		out = append(out, entry)
	}
	c.JSON(http.StatusOK, out)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	r := gin.Default()
	r.POST("/debit", postDebit)
	r.GET("/ledger", getLedger)

	log.Printf("Ledger Service running on :%s", port)
	r.Run(":" + port)
}
