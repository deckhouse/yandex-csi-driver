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

FROM golang:1.25.12-alpine3.24@sha256:56961d79ea8129efddcc0b8643fd8a5416b4e6228cfd477e3fd61deb2672c587 AS build
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

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

RUN apk add --no-cache ca-certificates \
                       e2fsprogs \
                       findmnt \
                       xfsprogs \
                       xfsprogs-extra \
                       blkid \
                       e2fsprogs-extra

COPY --from=build /go/bin/yandex-csi-driver /bin/

ENTRYPOINT ["/bin/yandex-csi-driver"]
