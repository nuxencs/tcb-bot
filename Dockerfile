FROM golang:latest as builder
COPY . /app
ENV GOOS=linux CGO_ENABLED=0
WORKDIR /app
RUN go build && ls -la

FROM alpine:3.16
COPY --from=builder /app/tcb-bot /app/
WORKDIR /app
CMD /app/tcb-bot