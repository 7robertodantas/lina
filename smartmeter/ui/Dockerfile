# Build Stage
FROM node:22-alpine AS build
    WORKDIR /app

    ARG NEXT_PUBLIC_BACKEND_WS_URL=ws://localhost:8080/ws

    COPY package.json package-lock.json* ./
    RUN npm install
    COPY . .
    RUN NEXT_PUBLIC_BACKEND_WS_URL=${NEXT_PUBLIC_BACKEND_WS_URL} npm run build

# Production Stage
FROM caddy:2.9-alpine
    COPY Caddyfile /etc/caddy/Caddyfile
    COPY --from=build /app/out /usr/share/caddy

    EXPOSE 80