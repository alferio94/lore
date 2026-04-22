FROM golang:1.25-alpine AS builder

ARG VERSION=dev

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -ldflags "-X main.version=${VERSION}" -o /lore ./cmd/lore

FROM alpine:3.22

RUN addgroup -S lore && adduser -S lore -G lore && apk add --no-cache curl

COPY --from=builder /lore /usr/local/bin/lore

USER lore

EXPOSE 7437

ENTRYPOINT ["/usr/local/bin/lore"]
CMD ["serve"]
