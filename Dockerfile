FROM golang:1.22-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /acb .

FROM scratch
COPY --from=builder /acb /acb
EXPOSE 8080
ENTRYPOINT ["/acb"]
