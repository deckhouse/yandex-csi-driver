# Copyright 2020 DigitalOcean
# Copyright 2020 Flant
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

FROM golang:1.15-alpine as build
RUN apk add git

WORKDIR /go/src/app
ADD . /go/src/app

ARG OS="linux"
ARG ARCH="amd64"

RUN export VERSION=$(cat VERSION)
RUN export COMMIT=$(git rev-parse HEAD)
RUN export GIT_TREE_STATE=$(git diff --quiet && echo 'clean' || echo 'dirty')

RUN CGO_ENABLED=0 GOOS=$OS GOARCH=$ARCH go build -a \
    -ldflags '-X github.com/deckhouse/yandex-csi-driver/driver.version=${VERSION} -X github.com/deckhouse/yandex-csi-driver/driver.commit=${COMMIT} -X github.com/deckhouse/yandex-csi-driver/driver.gitTreeState=${GIT_TREE_STATE}' \
    -o /go/bin/yandex-csi-driver \
    github.com/deckhouse/yandex-csi-driver/cmd/yandex-csi-driver

FROM alpine:3.20@sha256:0a4eaa0eecf5f8c050e5bba433f58c052be7587ee8af3e8b3910ef9ab5fbe9f5

RUN apk add --no-cache ca-certificates \
                       e2fsprogs \
                       findmnt \
                       xfsprogs \
                       blkid \
                       e2fsprogs-extra

COPY --from=build /go/bin/yandex-csi-driver /bin/

ENTRYPOINT ["/bin/yandex-csi-driver"]
