# Multi-stage build for smaller image
FROM golang:1.22-alpine AS builder

WORKDIR /workspace

# Copy go mod files
COPY go.mod go.mod
COPY go.sum go.sum*

# Copy source code (needed for go mod tidy to work)
COPY cmd/ cmd/
COPY pkg/ pkg/

# Download dependencies and ensure go.sum is complete
RUN go mod download && go mod tidy

# Build the scheduler
RUN CGO_ENABLED=0 GOOS=linux go build -a -o kube-scheduler ./cmd/scheduler

# Final stage - minimal image
FROM gcr.io/distroless/static:nonroot

WORKDIR /

# Copy the binary from builder
COPY --from=builder /workspace/kube-scheduler .

USER 65532:65532

ENTRYPOINT ["/kube-scheduler"]
