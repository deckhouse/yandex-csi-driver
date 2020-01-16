FROM alpine:3.10

RUN apk add --no-cache ca-certificates \
                       e2fsprogs \
                       findmnt \
                       xfsprogs \
                       blkid \
                       e2fsprogs-extra

ADD yandex-csi-driver /bin/

ENTRYPOINT ["/bin/yandex-csi-driver"]
