# Scripts

This directory contains deployment and build scripts for the LNPay system.

## Scripts

### `deploy.sh`
Automated deployment script for local or remote environments.

**Usage:**
```bash
# Local deployment
./scripts/deploy.sh local

# Remote deployment
./scripts/deploy.sh remote user@hostname
./scripts/deploy.sh remote user@hostname -p 2222

# Help
./scripts/deploy.sh help
```

The script will:
- Copy required files to the target machine
- Verify prerequisites (Docker, docker-compose)
- Set up certificates
- Pull Docker images
- Provide instructions to start services

### `build-and-push.sh`
Builds and pushes Docker images to a registry.

**Usage:**
```bash
./scripts/build-and-push.sh [registry/repository] [tag] [platforms]
```

**Examples:**
```bash
# Build and push to Docker Hub
./scripts/build-and-push.sh docker.io/username/lnpay latest

# With specific tag
./scripts/build-and-push.sh docker.io/username/lnpay v1.0.0

# For specific platforms
./scripts/build-and-push.sh docker.io/username/lnpay latest linux/amd64,linux/arm64
```

See `DOCKER_PUBLISH.md` for detailed documentation.

## Notes

- Scripts automatically detect the project root directory
- All paths are relative to the project root
- Scripts can be run from any directory

