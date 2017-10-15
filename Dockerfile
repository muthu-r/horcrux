FROM golang:1.9-alpine as horcrux.builder
COPY . /go/src/github.com/muthu-r/horcrux
WORKDIR /go/src/github.com/muthu-r/horcrux
RUN set -ex && apk add --no-cache --virtual .build_deps gcc libc-dev git make
WORKDIR /go/src/github.com/muthu-r/horcrux/horcrux-dv
RUN go clean && go get && go install  --ldflags '-extldflafs "-static"'
RUN apk add --no-cache --virtual fuse
RUN apk del .build_deps
WORKDIR /
RUN rm -rf /go/src && rm -rf /go/pkg
CMD ["/horcrux-dv"]
