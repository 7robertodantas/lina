FROM eclipse-mosquitto:2-openssl

# Create necessary directories
RUN mkdir -p /mosquitto/config /mosquitto/data /mosquitto/certs

# Copy configuration files
COPY config/mosquitto.conf /mosquitto/config/mosquitto.conf
COPY config/entrypoint.sh /mosquitto/config/entrypoint.sh
COPY config/healthcheck.sh /mosquitto/config/healthcheck.sh
COPY generate-dynsec.sh /mosquitto/generate-dynsec.sh

# Note: Certificates should NOT be copied into the image for production
# They should be mounted as volumes at runtime for security and flexibility
# This allows:
# - Different certificates per environment (dev/staging/prod)
# - Certificate rotation without rebuilding images
# - Keeping sensitive keys out of image layers

# Make scripts executable
RUN chmod +x /mosquitto/config/entrypoint.sh \
    && chmod +x /mosquitto/config/healthcheck.sh \
    && chmod +x /mosquitto/generate-dynsec.sh

# Note: Entrypoint will run as whatever user the base image uses
# If permissions are set correctly on the host (644), the entrypoint will work
# If running as root, the entrypoint can fix permissions automatically
# Set entrypoint (will fix permissions if running as root, otherwise relies on host permissions)
ENTRYPOINT ["/mosquitto/config/entrypoint.sh"]

# Default command (mosquitto will run as mosquitto user as per base image)
CMD ["mosquitto", "-c", "/mosquitto/config/mosquitto.conf"]
