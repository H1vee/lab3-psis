ARG GO_VERSION=1.25

FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./

ARG APP_VERSION=1.0.0

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${APP_VERSION}" \
    -o /out/teapot-api .

FROM scratch AS runtime

ARG APP_VERSION=1.0.0

LABEL org.opencontainers.image.title="teapot-api" \
      org.opencontainers.image.description="JSON API over the tea catalogue in PostgreSQL" \
      org.opencontainers.image.version="${APP_VERSION}"

COPY --from=builder /out/teapot-api /teapot-api

ENV API_ADDR=":8080"

USER 10001:10001

EXPOSE 8080

ENTRYPOINT ["/teapot-api"]
CMD []
