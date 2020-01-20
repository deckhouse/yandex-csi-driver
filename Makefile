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

NAME=yandex-csi-driver
OS ?= linux
ifeq ($(strip $(shell git status --porcelain 2>/dev/null)),)
  GIT_TREE_STATE=clean
else
  GIT_TREE_STATE=dirty
endif
COMMIT ?= $(shell git rev-parse HEAD)
BRANCH ?= $(shell git rev-parse --abbrev-ref HEAD)
LDFLAGS ?= -X github.com/flant/yandex-csi-driver/driver.version=${VERSION} -X github.com/flant/yandex-csi-driver/driver.commit=${COMMIT} -X github.com/flant/yandex-csi-driver/driver.gitTreeState=${GIT_TREE_STATE}
PKG ?= github.com/flant/yandex-csi-driver/cmd/yandex-csi-driver

VERSION ?= $(shell cat VERSION)
DOCKER_REPO ?= flant/yandex-csi-plugin

all: compile build

.PHONY: compile
compile:
	@echo "==> Building the project"
	@docker run --rm -it -e GOOS=${OS} -e GOARCH=amd64 -v ${PWD}/:/app -w /app golang:1.13-alpine sh -c 'apk add git && go build -o cmd/yandex-csi-driver/${NAME} -ldflags "$(LDFLAGS)" ${PKG}'

.PHONY: build
build:
	@echo "==> Building the docker image"
	@docker build -t $(DOCKER_REPO):$(VERSION) cmd/yandex-csi-driver -f cmd/yandex-csi-driver/Dockerfile
