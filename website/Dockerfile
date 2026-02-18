FROM node:20-alpine AS base
WORKDIR /app

# Install dependencies
FROM base AS deps
COPY package.json ./
RUN npm install

# Development server
FROM base AS dev
COPY --from=deps /app/node_modules ./node_modules
COPY . .
EXPOSE 3001
CMD ["npm", "run", "dev"]

# Build
FROM base AS builder
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN npm run build

# Production — serve static files
FROM base AS runner
RUN npm install -g serve
COPY --from=builder /app/out ./out
EXPOSE 3001
CMD ["serve", "out", "-p", "3001"]
