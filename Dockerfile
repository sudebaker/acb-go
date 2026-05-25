FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go mod tidy && CGO_ENABLED=0 go build -o /acb .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
RUN adduser -D -u 1000 acb
COPY --from=builder /acb /acb
USER acb
EXPOSE 8090
ENTRYPOINT ["/acb"]