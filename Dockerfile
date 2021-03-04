FROM golang:1.14
RUN apt-get update

WORKDIR /go/src/github.com/gashirar/sak-server/
COPY main.go .
RUN go get
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o sak-server .

###############################
# Exec container
###############################

FROM ubuntu:18.04
RUN apt-get update \
 && apt-get install stress curl wget net-tools vim dnsutils iputils-ping -y

COPY --from=0 /go/src/github.com/gashirar/sak-server/sak-server /sak-server
RUN mkdir /probe && \
    echo "liveness ok" > /probe/liveness.html && \
    echo "readiness ok" > /probe/readiness.html

CMD ["/sak-server"]
