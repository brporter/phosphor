# Stage 1: Build web frontend
FROM node:22-alpine AS web-build
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: Build Go binaries
FROM golang:1.23-alpine AS go-build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=web-build /app/web/dist web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /relay ./cmd/relay

# Stage 3: Final minimal image
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go-build /relay /relay
COPY --from=web-build /app/web/dist /web/dist
EXPOSE 8080
ENV ADDR=":8080"
ENTRYPOINT ["/relay"]
