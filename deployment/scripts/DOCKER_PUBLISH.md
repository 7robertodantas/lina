# Publishing Docker Images

This guide explains how to build and publish Docker images to a Docker registry for use across multiple machines.

## Prerequisites

1. Docker installed and running (with Buildx support)
2. Access to a Docker registry (Docker Hub, GitHub Container Registry, or private registry)
3. Logged in to your registry:
   ```bash
   docker login
   # or for private registries:
   docker login your-registry.com
   ```

## Multi-Architecture Support

The build script supports **multi-architecture builds** by default, building for both `linux/amd64` and `linux/arm64` platforms. This allows your images to run on:
- Intel/AMD 64-bit systems (amd64)
- Apple Silicon and ARM-based systems (arm64)

The script uses Docker Buildx to create multi-platform images. The first time you run it, it will automatically create a buildx builder instance.

## Quick Start

### 1. Build and Push All Images

Use the provided script to build and push all images:

```bash
# Make the script executable
chmod +x scripts/build-and-push.sh

# Build and push to Docker Hub (replace 'username' with your Docker Hub username)
# By default, builds for both amd64 and arm64
./scripts/build-and-push.sh docker.io/username/lnpay latest

# Or use a specific tag
./scripts/build-and-push.sh docker.io/username/lnpay v1.0.0

# For a private registry
./scripts/build-and-push.sh registry.example.com/lnpay latest

# Custom platforms (optional third parameter)
# Build only for amd64
./scripts/build-and-push.sh docker.io/username/lnpay latest linux/amd64

# Build for specific platforms
./scripts/build-and-push.sh docker.io/username/lnpay latest linux/amd64,linux/arm64,linux/arm/v7
```

### 2. Using Published Images

On other machines, use the production docker-compose file:

```bash
# Set environment variables
export DOCKER_REGISTRY=docker.io/username/lnpay
export IMAGE_TAG=latest

# Pull and run
docker-compose -f docker-compose.prod.yml pull
docker-compose -f docker-compose.prod.yml up -d
```

Or create a `.env` file with:

```env
DOCKER_REGISTRY=docker.io/username/lnpay
IMAGE_TAG=latest
```

Then run:

```bash
docker-compose -f docker-compose.prod.yml up -d
```

## Manual Build and Push

If you prefer to build and push images manually:

### Single Architecture (Current Platform Only)

```bash
# Build images for current platform only
docker build -t docker.io/username/lnpay-caddy:latest -f ./caddy/Dockerfile ./caddy
docker build -t docker.io/username/lnpay-redis:latest -f ./redis/Dockerfile ./redis
docker build -t docker.io/username/lnpay-mosquitto:latest -f ./mosquitto/Dockerfile ./mosquitto
docker build -t docker.io/username/lnpay-device:latest -f ./services/Dockerfile --build-arg SERVICE=device .
docker build -t docker.io/username/lnpay-ledger:latest -f ./services/Dockerfile --build-arg SERVICE=ledger .
docker build -t docker.io/username/lnpay-consumption:latest -f ./services/Dockerfile --build-arg SERVICE=consumption .
docker build -t docker.io/username/lnpay-lightning:latest -f ./services/Dockerfile --build-arg SERVICE=lightning .
docker build -t docker.io/username/lnpay-smartmeter:latest -f ./smartmeter/Dockerfile .

# Push images
docker push docker.io/username/lnpay-caddy:latest
docker push docker.io/username/lnpay-redis:latest
docker push docker.io/username/lnpay-mosquitto:latest
docker push docker.io/username/lnpay-device:latest
docker push docker.io/username/lnpay-ledger:latest
docker push docker.io/username/lnpay-consumption:latest
docker push docker.io/username/lnpay-lightning:latest
docker push docker.io/username/lnpay-smartmeter:latest
```

### Multi-Architecture Build (Recommended)

For multi-architecture builds, use Docker Buildx:

```bash
# Set up buildx builder (one-time setup)
docker buildx create --name multiarch-builder --use
docker buildx inspect --bootstrap

# Build and push for multiple platforms
docker buildx build --platform linux/amd64,linux/arm64 \
  -t docker.io/username/lnpay-caddy:latest \
  -f ./caddy/Dockerfile \
  --push ./caddy

docker buildx build --platform linux/amd64,linux/arm64 \
  -t docker.io/username/lnpay-redis:latest \
  -f ./redis/Dockerfile \
  --push ./redis

docker buildx build --platform linux/amd64,linux/arm64 \
  -t docker.io/username/lnpay-mosquitto:latest \
  -f ./mosquitto/Dockerfile \
  --push ./mosquitto

docker buildx build --platform linux/amd64,linux/arm64 \
  -t docker.io/username/lnpay-device:latest \
  -f ./services/Dockerfile \
  --build-arg SERVICE=device \
  --push .

docker buildx build --platform linux/amd64,linux/arm64 \
  -t docker.io/username/lnpay-ledger:latest \
  -f ./services/Dockerfile \
  --build-arg SERVICE=ledger \
  --push .

docker buildx build --platform linux/amd64,linux/arm64 \
  -t docker.io/username/lnpay-consumption:latest \
  -f ./services/Dockerfile \
  --build-arg SERVICE=consumption \
  --push .

docker buildx build --platform linux/amd64,linux/arm64 \
  -t docker.io/username/lnpay-lightning:latest \
  -f ./services/Dockerfile \
  --build-arg SERVICE=lightning \
  --push .

docker buildx build --platform linux/amd64,linux/arm64 \
  -t docker.io/username/lnpay-smartmeter:latest \
  -f ./smartmeter/Dockerfile \
  --push .
```

## Registry Options

### Docker Hub

```bash
./scripts/build-and-push.sh docker.io/username/lnpay latest
```

### GitHub Container Registry

```bash
./scripts/build-and-push.sh ghcr.io/username/lnpay latest
```

### Private Registry

```bash
./scripts/build-and-push.sh registry.example.com/lnpay latest
```

## Image Naming Convention

Images are named as: `{REGISTRY}-{SERVICE}:{TAG}`

Examples:
- `docker.io/username/lnpay-caddy:latest`
- `docker.io/username/lnpay-device:v1.0.0`
- `registry.example.com/lnpay-redis:latest`

## Development vs Production

- **Development**: Use `docker-compose.dev.yml` (builds images locally)
- **Production**: Use `docker-compose.prod.yml` (pulls pre-built images)

You can still use `docker-compose.dev.yml` for local development:

```bash
docker-compose -f docker-compose.dev.yml up --build
```

## Updating Images

When you make changes to your code:

1. Rebuild and push:
   ```bash
   ./scripts/build-and-push.sh docker.io/username/lnpay v1.1.0
   ```

2. On other machines, pull the new version:
   ```bash
   export IMAGE_TAG=v1.1.0
   docker-compose -f docker-compose.prod.yml pull
   docker-compose -f docker-compose.prod.yml up -d
   ```

## CI/CD Integration

You can integrate the build script into your CI/CD pipeline:

```yaml
# Example GitHub Actions
- name: Build and push Docker images
  run: |
    docker login -u ${{ secrets.DOCKER_USERNAME }} -p ${{ secrets.DOCKER_PASSWORD }}
    ./scripts/build-and-push.sh docker.io/${{ secrets.DOCKER_USERNAME }}/lnpay ${{ github.ref_name }}
```

