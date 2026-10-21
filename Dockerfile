FROM golang:1.26.8 AS builder

WORKDIR /src

COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/ ./

RUN mkdir -p /out

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/migrate ./cmd/migrate

FROM debian:bookworm-slim AS runtime

WORKDIR /app

COPY --from=builder /out/api ./api
COPY --from=builder /out/migrate ./migrate
COPY --from=builder /src/migrations ./migrations

EXPOSE 8080

CMD ["./api"]