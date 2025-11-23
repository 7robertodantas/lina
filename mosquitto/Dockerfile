FROM eclipse-mosquitto:2-openssl

# Create necessary directories
RUN mkdir -p /mosquitto/config /mosquitto/data /mosquitto/certs

# Copy configuration files
COPY config/mosquitto.conf /mosquitto/config/mosquitto.conf
COPY config/entrypoint.sh /mosquitto/config/entrypoint.sh
COPY config/healthcheck.sh /mosquitto/config/healthcheck.sh
COPY generate-dynsec.sh /mosquitto/generate-dynsec.sh
COPY generate-passwd-container.sh /mosquitto/generate-passwd.sh

# Copy certificates directory (should exist with certificates before building)
COPY certs/ /mosquitto/certs/

# Make scripts executable
RUN chmod +x /mosquitto/config/entrypoint.sh \
    && chmod +x /mosquitto/config/healthcheck.sh \
    && chmod +x /mosquitto/generate-dynsec.sh \
    && chmod +x /mosquitto/generate-passwd.sh

# Set entrypoint
ENTRYPOINT ["/mosquitto/config/entrypoint.sh"]

# Default command
CMD ["mosquitto", "-c", "/mosquitto/config/mosquitto.conf"]
