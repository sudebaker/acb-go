FROM golang:1.24-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go mod tidy && go build -o /acb .

FROM alpine:3.19
RUN apk add --no-cache sqlite ca-certificates
RUN adduser -D -u 1000 acb \
    && mkdir -p /var/lib/acb \
    && chown acb:acb /var/lib/acb
COPY --from=builder /acb /acb
USER acb
EXPOSE 8090
ENTRYPOINT ["/acb"]
