#################################
#     RTMP Server Dockerfile    #
#################################

# Builder
FROM golang:alpine AS builder

    ## Copy files
    ADD . /root

    ## Compile
    WORKDIR /root
    RUN go build -o rtmp-server


# Runner
FROM alpine AS runner

    ## Install common libraries
    RUN apk add gcompat

    ## Copy binary
    COPY --from=builder /root/rtmp-server /usr/bin/rtmp-server

    # Expose ports
    EXPOSE 1935
    EXPOSE 443

    # Entry point
    ENTRYPOINT ["/usr/bin/rtmp-server"]
