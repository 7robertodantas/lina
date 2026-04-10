# Scripts

This directory contains deployment and build scripts for the LINA system.

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
./scripts/build-and-push.sh docker.io/username/lina latest

# With specific tag
./scripts/build-and-push.sh docker.io/username/lina v1.0.0

# For specific platforms
./scripts/build-and-push.sh docker.io/username/lina latest linux/amd64,linux/arm64
```

See `DOCKER_PUBLISH.md` for detailed documentation.

### `edge-up-default.sh`
Starts `docker-compose.evaluation.edge.yml` using Docker-managed named volumes.

**Usage:**
```bash
./deployment/scripts/edge-up-default.sh
./deployment/scripts/edge-up-default.sh --force-recreate
```

### `edge-up-ssd.sh`
Starts edge evaluation compose with SSD bind mounts override (`docker-compose.evaluation.edge.ssd.yml`).

**Usage:**
```bash
export EDGE_DATA_ROOT=/Volumes/MySSD/lnpay-edge-data
./deployment/scripts/edge-up-ssd.sh
```

### Optional shell aliases (zsh/bash)
```bash
alias edge-up-default='$PWD/deployment/scripts/edge-up-default.sh'
alias edge-up-ssd='EDGE_DATA_ROOT=/Volumes/MySSD/lnpay-edge-data $PWD/deployment/scripts/edge-up-ssd.sh'
```

## Notes

- Scripts automatically detect the project root directory
- All paths are relative to the project root
- Scripts can be run from any directory

