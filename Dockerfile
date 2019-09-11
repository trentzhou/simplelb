FROM golang:latest
RUN env CGO_ENABLED=0 GOOS=linux go get -u github.com/trentzhou/simplelb

FROM alpine:latest  
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=0 /go/bin/simplelb .
CMD ["./simplelb"]  