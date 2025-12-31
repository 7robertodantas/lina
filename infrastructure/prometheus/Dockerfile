FROM prom/prometheus:v3.8.0

# Copy entrypoint script and template
USER root
COPY entrypoint.sh /etc/prometheus/entrypoint.sh
COPY prometheus.template.yml /etc/prometheus/prometheus.template.yml
RUN chmod +x /etc/prometheus/entrypoint.sh
USER nobody
