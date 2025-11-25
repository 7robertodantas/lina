# Smart Meter UI

A Next.js-based UI for smart meter simulation with MQTT connectivity.

## Environment Variables

This project uses environment variables to configure the MQTT broker connection and other settings.

### Setup

1. Copy the example environment file:
   ```bash
   cp .env.example .env.local
   ```

2. Edit `.env.local` with your actual values:
   ```env
   NEXT_PUBLIC_MQTT_BROKER_URL=wss://your-mqtt-broker.com
   NEXT_PUBLIC_MQTT_USERNAME=your-username
   NEXT_PUBLIC_MQTT_PASSWORD=your-password
   NEXT_PUBLIC_DEFAULT_DEVICE_ID=your-device-id
   ```

### Available Variables

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `NEXT_PUBLIC_MQTT_BROKER_URL` | WebSocket URL for MQTT broker | Yes | `wss://mqtt.example.com` |
| `NEXT_PUBLIC_MQTT_USERNAME` | MQTT broker username | No | - |
| `NEXT_PUBLIC_MQTT_PASSWORD` | MQTT broker password | No | - |
| `NEXT_PUBLIC_DEFAULT_DEVICE_ID` | Default device identifier | No | `smart-meter-001` |

### Important Notes

- **`NEXT_PUBLIC_` prefix**: Variables that need to be accessible in the browser must be prefixed with `NEXT_PUBLIC_`
- **`.env.local`**: This file is git-ignored and used for local development
- **`.env.example`**: Template file that should be committed to version control
- **Build time**: Environment variables are embedded at build time for production builds
- **Development**: Changes to `.env.local` require restarting the dev server (`pnpm dev`)

## Development

```bash
# Install dependencies
pnpm install

# Start development server
pnpm dev
```

## Production Build

```bash
# Build for production
pnpm build

# Start production server
pnpm start
```

For production deployments (Vercel, Docker, etc.), set environment variables in your hosting platform's dashboard or configuration.
