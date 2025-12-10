FROM prom/prometheus:latest

# Copy entrypoint script
USER root
COPY entrypoint.sh /etc/prometheus/entrypoint.sh
RUN chmod +x /etc/prometheus/entrypoint.sh
USER nobody

