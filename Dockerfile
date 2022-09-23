FROM golang:1.19.1-alpine AS builder

WORKDIR /go/src/app
COPY ./go.* ./
RUN export http_proxy=http://10.94.4.6:22222\
 && export https_proxy=$http_proxy\
 && export GOPROXY="https://goproxy.io,direct"\
 && go mod download \
 && unset http_proxy\
 && unset https_proxy
COPY . .
RUN export http_proxy=http://10.94.4.6:22222\
 && export https_proxy=$http_proxy\
 && export GOPROXY="https://goproxy.io,direct"\
 && go get github.com/chenwyi2/kubeloadbalancer \
 && go get github.com/coredns/kubeapi \
 && go generate \
 && go build -o /go/bin/coredns \
 && unset http_proxy\
 && unset https_proxy


FROM alpine:3.16 AS runner

WORKDIR /go/bin
ENV PATH=/go/bin:$PATH
COPY --from=builder /go/bin/coredns /go/bin/coredns

EXPOSE 53 53/udp
ENTRYPOINT ["coredns"]