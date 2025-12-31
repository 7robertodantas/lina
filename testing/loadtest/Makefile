.PHONY: help install run run-custom clean httpdevices-start httpdevices-stop httpdevices-wait

# Get the directory where this Makefile is located
MAKEFILE_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

# Configuration
HTTPDEVICE_PORT ?= 3000
HTTPDEVICE_BIN ?= httpdevices
HTTPDEVICE_BASE_URL ?= http://localhost:$(HTTPDEVICE_PORT)
MQTT_BROKER ?= ssl://localhost:8883
PID_FILE := $(MAKEFILE_DIR)httpdevices.pid
LOG_FILE := $(MAKEFILE_DIR)httpdevices.log

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

httpdevices-start: ## Start httpdevices service in background
	@echo "Starting httpdevices service..."
	@if pgrep -f "$(HTTPDEVICE_BIN)" > /dev/null; then \
		echo "httpdevices is already running"; \
	else \
		cd $(MAKEFILE_DIR)httpdevices && MQTT_BROKER=$(MQTT_BROKER) LISTEN_ADDR=:$(HTTPDEVICE_PORT) go run . > $(LOG_FILE) 2>&1 & echo $$! > $(PID_FILE) && \
		echo "httpdevices started (PID: $$(cat $(PID_FILE)))"; \
	fi

httpdevices-wait: ## Wait for httpdevices to be ready
	@echo "Waiting for httpdevices to be ready..."
	@timeout=30; \
	while [ $$timeout -gt 0 ]; do \
		if curl -s $(HTTPDEVICE_BASE_URL)/devices/test/connect > /dev/null 2>&1 || \
		   curl -s $(HTTPDEVICE_BASE_URL) > /dev/null 2>&1; then \
			echo "httpdevices is ready"; \
			exit 0; \
		fi; \
		sleep 1; \
		timeout=$$((timeout - 1)); \
	done; \
	echo "Timeout waiting for httpdevices to start"; \
	exit 1

httpdevices-stop: ## Stop httpdevices service
	@if [ -f $(PID_FILE) ]; then \
		pid=$$(cat $(PID_FILE)); \
		if ps -p $$pid > /dev/null 2>&1; then \
			kill $$pid && echo "httpdevices stopped (PID: $$pid)"; \
		else \
			echo "httpdevices process not found"; \
		fi; \
		rm -f $(PID_FILE) $(LOG_FILE); \
	else \
		pkill -f "$(HTTPDEVICE_BIN)" && echo "httpdevices stopped" || echo "httpdevices not running"; \
		rm -f $(LOG_FILE); \
	fi

run: httpdevices-start httpdevices-wait ## Run the load test with default settings (starts httpdevices first)
	@echo "Running k6 load test..."
	@HTTPDEVICE_BASE_URL=$(HTTPDEVICE_BASE_URL) k6 run --insecure-skip-tls-verify loadtest.js; \
	test_exit=$$?; \
	$(MAKE) httpdevices-stop; \
	exit $$test_exit

run-custom: httpdevices-start httpdevices-wait ## Run with custom environment variables (starts httpdevices first)
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
	$(MAKE) httpdevices-stop; \
	exit $$test_exit

clean: httpdevices-stop ## Clean test result files and stop httpdevices
	rm -f $(MAKEFILE_DIR)*.json $(MAKEFILE_DIR)*.html $(MAKEFILE_DIR)*.csv

