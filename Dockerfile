# Build Stage
FROM golang:1.24 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.Version=${VERSION}" -o controller cmd/controller/main.go

# Runtime Stage
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /app/controller .
USER 65532:65532
ENTRYPOINT ["/controller"]
