# Monitoring Stack

This directory contains the configuration for Prometheus and Grafana to monitor the LNPay production deployment.

## Architecture

- **Production Server**: Runs `docker-compose.prod.yml` with cAdvisor and node-exporter
- **Monitoring Server**: Runs `docker-compose.monitoring.yml` with Prometheus and Grafana

## Setup

### On Production Server

1. Deploy the production stack with monitoring exporters:
   ```bash
   docker-compose -f docker-compose.prod.yml up -d
   ```

2. Verify exporters are running:
   - cAdvisor: `http://production-server:8081/metrics`
   - node-exporter: `http://production-server:9100/metrics`

3. Ensure ports 8081 and 9100 are accessible from the monitoring server (firewall rules).

### On Monitoring Server

1. Set the `TARGET_HOST` environment variable to your production server IP address or hostname:
   ```bash
   export TARGET_HOST=192.168.1.100
   # or
   export TARGET_HOST=production.example.com
   ```

   Alternatively, you can create a `.env` file in the project root:
   ```
   TARGET_HOST=192.168.1.100
   ```

2. Start the monitoring stack:
   ```bash
   docker-compose -f docker-compose.monitoring.yml up -d
   ```

3. Access the services:
   - Grafana: `http://monitoring-server:3000` (default: admin/admin)
   - Prometheus: `http://monitoring-server:9090`

## Configuration

### Prometheus

Edit `monitoring/prometheus/prometheus.yml` to:
- Adjust scrape intervals
- Add more targets
- Configure alerting rules

### Grafana

- Default admin credentials: `admin` / `admin` (change via environment variables)
- Prometheus datasource is auto-provisioned
- Add dashboards via the UI or place JSON files in `monitoring/grafana/dashboards/`

## Recommended Dashboards

Import these Grafana dashboards for comprehensive monitoring:

1. **Node Exporter Full** (ID: 1860) - Host metrics
2. **Docker Container & Host Metrics** (ID: 10619) - Container metrics from cAdvisor
3. **Kubernetes / Docker Container Metrics** (ID: 8588) - Alternative container dashboard

## Security Considerations

- Change default Grafana admin password
- Use reverse proxy (e.g., Caddy) with TLS for production access
- Restrict network access to monitoring ports
- Consider using authentication for Prometheus if exposing it

