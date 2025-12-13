.PHONY: help install run run-custom clean httpdevice-start httpdevice-stop httpdevice-wait

# Get the directory where this Makefile is located
MAKEFILE_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

# Configuration
HTTPDEVICE_PORT ?= 3000
HTTPDEVICE_BIN ?= httpdevice
HTTPDEVICE_BASE_URL ?= http://localhost:$(HTTPDEVICE_PORT)
MQTT_BROKER ?= ssl://localhost:8883
PID_FILE := $(MAKEFILE_DIR)httpdevice.pid
LOG_FILE := $(MAKEFILE_DIR)httpdevice.log

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

httpdevice-start: ## Start httpdevice service in background
	@echo "Starting httpdevice service..."
	@if pgrep -f "$(HTTPDEVICE_BIN)" > /dev/null; then \
		echo "httpdevice is already running"; \
	else \
		cd $(MAKEFILE_DIR)httpdevice && MQTT_BROKER=$(MQTT_BROKER) LISTEN_ADDR=:$(HTTPDEVICE_PORT) go run . > $(LOG_FILE) 2>&1 & echo $$! > $(PID_FILE) && \
		echo "httpdevice started (PID: $$(cat $(PID_FILE)))"; \
	fi

httpdevice-wait: ## Wait for httpdevice to be ready
	@echo "Waiting for httpdevice to be ready..."
	@timeout=30; \
	while [ $$timeout -gt 0 ]; do \
		if curl -s $(HTTPDEVICE_BASE_URL)/devices/test/connect > /dev/null 2>&1 || \
		   curl -s $(HTTPDEVICE_BASE_URL) > /dev/null 2>&1; then \
			echo "httpdevice is ready"; \
			exit 0; \
		fi; \
		sleep 1; \
		timeout=$$((timeout - 1)); \
	done; \
	echo "Timeout waiting for httpdevice to start"; \
	exit 1

httpdevice-stop: ## Stop httpdevice service
	@if [ -f $(PID_FILE) ]; then \
		pid=$$(cat $(PID_FILE)); \
		if ps -p $$pid > /dev/null 2>&1; then \
			kill $$pid && echo "httpdevice stopped (PID: $$pid)"; \
		else \
			echo "httpdevice process not found"; \
		fi; \
		rm -f $(PID_FILE) $(LOG_FILE); \
	else \
		pkill -f "$(HTTPDEVICE_BIN)" && echo "httpdevice stopped" || echo "httpdevice not running"; \
		rm -f $(LOG_FILE); \
	fi

run: httpdevice-start httpdevice-wait ## Run the load test with default settings (starts httpdevice first)
	@echo "Running k6 load test..."
	@HTTPDEVICE_BASE_URL=$(HTTPDEVICE_BASE_URL) k6 run --insecure-skip-tls-verify loadtest.js; \
	test_exit=$$?; \
	$(MAKE) httpdevice-stop; \
	exit $$test_exit

run-custom: httpdevice-start httpdevice-wait ## Run with custom environment variables (starts httpdevice first)
	@echo "Running k6 load test with custom settings..."
	@API_BASE_URL=$(API_BASE_URL) \
	HTTPDEVICE_BASE_URL=$(HTTPDEVICE_BASE_URL) \
	MQTT_BROKER=$(MQTT_BROKER) \
	MQTT_TLS_PORT=$(MQTT_TLS_PORT) \
	USAGE_REPORT_INTERVAL=$(USAGE_REPORT_INTERVAL) \
	AUTHORIZE_REQUEST_MSAT=$(AUTHORIZE_REQUEST_MSAT) \
	HEARTBEAT_INTERVAL=$(HEARTBEAT_INTERVAL) \
	k6 run --insecure-skip-tls-verify loadtest.js; \
	test_exit=$$?; \
	$(MAKE) httpdevice-stop; \
	exit $$test_exit

clean: httpdevice-stop ## Clean test result files and stop httpdevice
	rm -f $(MAKEFILE_DIR)*.json $(MAKEFILE_DIR)*.html $(MAKEFILE_DIR)*.csv

