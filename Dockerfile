# ---- build stage ----------------------------------------------------------
FROM golang:1.23-alpine AS build
WORKDIR /src

# Copy sources and resolve dependencies. `go mod tidy` generates go.sum on the
# first build (handy here since Go isn't installed on the host yet).
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /bin/api ./cmd/api

# ---- run stage ------------------------------------------------------------
FROM alpine:3.20
RUN adduser -D -u 10001 appuser
COPY --from=build /bin/api /usr/local/bin/api
USER appuser
EXPOSE 8080
ENTRYPOINT ["api"]
