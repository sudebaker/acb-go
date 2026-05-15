FROM golang:1.24-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go mod tidy && go build -o /acb .

FROM alpine:3.19
RUN apk add --no-cache sqlite
COPY --from=builder /acb /acb
EXPOSE 8080
ENTRYPOINT ["/acb"]
