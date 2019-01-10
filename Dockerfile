FROM golang:1.11.1-alpine as builder

RUN apk add git && go get -u github.com/golang/dep/cmd/dep
ADD . /go/src/github.com/zhezack/pgdeltastream
WORKDIR /go/src/github.com/zhezack/pgdeltastream
RUN go build .

FROM alpine:3.8
RUN apk add ca-certificates
COPY --from=builder /go/src/github.com/zhezack/pgdeltastream /app/
WORKDIR /app
CMD  ["./pgdeltastream"]
