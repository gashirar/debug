FROM rust:1.59 AS rustbase

FROM rustbase AS bandwhich
# renovate: datasource=crate depName=bandwhich
ARG BANDWHICH_VERSION=0.20.0
RUN set -x && \
    cargo install bandwhich --version "${BANDWHICH_VERSION}" && \
    /usr/local/cargo/bin/bandwhich --version

FROM rustbase AS dog
# renovate: datasource=github-releases depName=ogham/dog
ARG DOG_VERSION=v0.1.0
RUN set -x && \
    git clone -b "${DOG_VERSION}" --depth 1 https://github.com/ogham/dog.git && \
    cd dog && \
    cargo build --release && \
    ./target/release/dog --version

FROM golang:1.18 AS hey
# renovate: datasource=github-releases depName=rakyll/hey
ARG HEY_VERSION=v0.1.4
ARG TARGETOS
ARG TARGETARCH
RUN set -x && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go install "github.com/rakyll/hey@${HEY_VERSION}"


FROM golang:1.18 AS sak-server
RUN apt-get update

WORKDIR /go/src/github.com/gashirar/sak-server/
COPY main.go .
COPY go.mod .
RUN go get
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o sak-server .

###############################
# Exec container
###############################

FROM ubuntu:20.04
RUN set -x && \
    apt update && \
    apt install -y \
        iperf \
        net-tools \
        iproute2 \
        traceroute \
        openssh-client \
        iputils-ping \
        dnsutils \
        iptables \
        tcpdump \
        nmap \
        netcat \
        iperf3 \
        less \
        tree \
        vim \
        strace \
        curl \
        bash \
        sysstat \
        iotop \
        htop \
        sysbench \
        net-tools \
        wget \
        iptraf-ng \
        iptraf \
        stress \
        && \
    rm -rf /var/lib/apt/lists/*


RUN set -x && \
    curl -L -o /usr/local/bin/kubectl "https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl" && \
    chmod +x /usr/local/bin/kubectl

# renovate: datasource=github-releases depName=muesli/duf
ARG DUF_VERSION=0.8.1
RUN set -x && \
    curl -L -o duf.deb "https://github.com/muesli/duf/releases/download/v${DUF_VERSION}/duf_${DUF_VERSION}_linux_amd64.deb" && \
    dpkg -i duf.deb

# renovate: datasource=github-releases depName=sharkdp/bat
ARG BAT_VERSION=0.20.0
RUN set -x && \
    curl -L -o bat.deb "https://github.com/sharkdp/bat/releases/download/v${BAT_VERSION}/bat_${BAT_VERSION}_amd64.deb" && \
    dpkg -i bat.deb

COPY --from=hey /go/bin/hey /usr/local/bin/hey
COPY --from=bandwhich /usr/local/cargo/bin/bandwhich /usr/local/bin/bandwhich
COPY --from=dog /dog/target/release/dog /usr/local/bin/dog
COPY --from=sak-server /go/src/github.com/gashirar/sak-server/sak-server /sak-server
RUN mkdir /probe && \
    echo "liveness ok" > /probe/liveness.html && \
    echo "readiness ok" > /probe/readiness.html

CMD ["/sak-server"]
