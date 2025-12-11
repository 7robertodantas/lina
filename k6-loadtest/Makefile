.PHONY: help install run run-custom clean

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

install: ## Install k6 MQTT extension (requires xk6)
	@echo "Installing k6 MQTT extension..."
	@echo "Note: This requires xk6. Install it first:"
	@echo "  go install go.k6.io/xk6/cmd/xk6@latest"
	@echo ""
	@echo "Then build k6 with MQTT extension:"
	@echo "  xk6 build --with github.com/wgarcia4190/xk6-mqtt@latest"
	@echo ""
	@echo "Or use the pre-built k6 binary if available."

run: ## Run the load test with default settings (skips TLS verification for self-signed certs)
	k6 run --insecure-skip-tls-verify loadtest.js

run-custom: ## Run with custom environment variables (edit Makefile to customize)
	API_BASE_URL=$(API_BASE_URL) \
	MQTT_BROKER=$(MQTT_BROKER) \
	MQTT_TLS_PORT=$(MQTT_TLS_PORT) \
	USAGE_REPORT_INTERVAL=$(USAGE_REPORT_INTERVAL) \
	AUTHORIZE_REQUEST_MSAT=$(AUTHORIZE_REQUEST_MSAT) \
	HEARTBEAT_INTERVAL=$(HEARTBEAT_INTERVAL) \
	k6 run --insecure-skip-tls-verify loadtest.js

clean: ## Clean test result files
	rm -f *.json *.html *.csv

