# build base
FROM --platform=$BUILDPLATFORM golang:1.23-alpine3.20 AS app-base

WORKDIR /src

ENV SERVICE=tcb-bot
ARG VERSION=dev \
    REVISION=dev \
    BUILDTIME \
    TARGETOS TARGETARCH TARGETVARIANT

COPY go.mod go.sum ./
RUN go mod download
COPY . ./

# build tcb-bot
FROM --platform=$BUILDPLATFORM app-base AS tcb-bot
RUN --network=none --mount=target=. \
    export GOOS=$TARGETOS; \
    export GOARCH=$TARGETARCH; \
    [[ "$GOARCH" == "amd64" ]] && export GOAMD64=$TARGETVARIANT; \
    [[ "$GOARCH" == "arm" ]] && [[ "$TARGETVARIANT" == "v6" ]] && export GOARM=6; \
    [[ "$GOARCH" == "arm" ]] && [[ "$TARGETVARIANT" == "v7" ]] && export GOARM=7; \
    echo $GOARCH $GOOS $GOARM$GOAMD64; \
    go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${REVISION} -X main.date=${BUILDTIME}" -o /out/bin/tcb-bot cmd/tcb-bot/main.go

# build runner
FROM alpine:latest AS RUNNER
RUN apk add --no-cache ca-certificates curl tzdata jq

LABEL org.opencontainers.image.source = "https://github.com/nuxencs/tcb-bot" \
      org.opencontainers.image.licenses = "MIT" \
      org.opencontainers.image.base.name = "alpine:latest"

ENV HOME="/config" \
    XDG_CONFIG_HOME="/config" \
    XDG_DATA_HOME="/config"

WORKDIR /app
VOLUME /config

COPY --link --from=tcb-bot /out/bin/tcb-bot /usr/bin/

ENTRYPOINT ["/usr/bin/tcb-bot", "start", "--config", "/config"]