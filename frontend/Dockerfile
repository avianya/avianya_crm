# =============================================================================
# Multi-stage build for the React frontend.
#
# A React app compiles down to static files, so the runtime image needs a web
# server, not Node. Stage 1 builds; stage 2 is nginx serving the output.
# =============================================================================

# ---- Stage 1: build ---------------------------------------------------------
FROM node:24-alpine AS build

WORKDIR /app

# `npm ci` (not `npm install`) installs exactly what package-lock.json pins, so
# the image is reproducible. Copying only the manifests first keeps this layer
# cached until the dependencies themselves actually change.
COPY package.json package-lock.json ./
RUN npm ci

COPY . .

# Vite inlines VITE_* variables into the bundle at BUILD time, not at run time --
# so the API URL has to be supplied here as a build argument. Changing it means
# rebuilding the image; there is no runtime env var to tweak afterwards.
ARG VITE_API_BASE_URL=http://localhost:8080/api
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL

RUN npm run build

# ---- Stage 2: serve ---------------------------------------------------------
FROM nginx:1.29-alpine AS runtime

COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
