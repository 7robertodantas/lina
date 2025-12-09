# Build Stage
FROM node:22-alpine AS build
    WORKDIR /app

    COPY package.json package-lock.json* ./
    RUN npm install
    COPY . .
    RUN npm run build

# Production Stage
FROM caddy:2.9-alpine
    COPY Caddyfile /etc/caddy/Caddyfile
    COPY --from=build /app/out /usr/share/caddy

    EXPOSE 80