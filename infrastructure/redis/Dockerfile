FROM redis:8.4.0-alpine

# Copy Redis configuration
COPY redis.conf /usr/local/etc/redis/redis.conf

# Use the custom config
CMD ["redis-server", "/usr/local/etc/redis/redis.conf"]
