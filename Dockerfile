FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o webhook ./cmd/webhook

FROM scratch
COPY --from=builder /app/webhook /webhook
EXPOSE 8443
ENTRYPOINT ["/webhook"]
