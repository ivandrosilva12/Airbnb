# Build the production bundle, then serve it as static files with nginx.
# The browser-facing API/Keycloak URLs default to the host-published ports
# (localhost:8081 / keycloak:8080), so no build args are required for local use.
FROM node:20-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
