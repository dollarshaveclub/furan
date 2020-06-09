FROM golang:1.14-alpine

COPY . /tmp/furan
RUN cd /tmp/furan && \
CGO_ENABLED=0 go build -mod=vendor

FROM alpine:3.11

RUN mkdir -p /opt/migrations && \
apk --no-cache add ca-certificates && apk --no-cache upgrade
COPY --from=0 /tmp/furan/furan /usr/local/bin/furan

ENV MIGRATIONS_PATH /opt/migrations

CMD ["/usr/local/bin/furan"]