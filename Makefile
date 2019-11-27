# Tencent is pleased to support the open source community by making TKEStack available.
#
# Copyright (C) 2012-2019 Tencent. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License"); you may not use
# this file except in compliance with the License. You may obtain a copy of the
# License at
#
# https://opensource.org/licenses/Apache-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
# WARRANTIES OF ANY KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations under the License.
#

REGISTRY_NAME=docker.io/tkestack

IMAGE_TAGS=

# A "canary" image gets built if the current commit is the head of the remote "master" branch.
# That branch does not exist when building some other branch in TravisCI.
IMAGE_TAGS+=$(shell if [ "$$(git rev-list -n1 HEAD)" = "$$(git rev-list -n1 origin/master 2>/dev/null)" ]; then echo "canary"; fi)

# A "X.Y.Z-canary" image gets built if the current commit is the head of a "origin/release-X.Y.Z" branch.
# The actual suffix does not matter, only the "release-" prefix is checked.
IMAGE_TAGS+=$(shell git branch -r --points-at=HEAD | grep 'origin/release-' | grep -v -e ' -> ' | sed -e 's;.*/release-\(.*\);\1-canary;')

# A release image "vX.Y.Z" gets built if there is a tag of that format for the current commit.
# --abbrev=0 suppresses long format, only showing the closest tag.
IMAGE_TAGS+=$(shell tagged="$$(git describe --tags --match='v*' --abbrev=0)"; if [ "$$tagged" ] && [ "$$(git rev-list -n1 HEAD)" = "$$(git rev-list -n1 $$tagged)" ]; then echo $$tagged; fi)

# Images are named after the command contained in them.
IMAGE_NAME=$(REGISTRY_NAME)/volume-decorator

all: test volume-decorator

# Run tests
test: generate fmt vet revive 
	go test ./pkg/... ./cmd/... -coverprofile cover.out

# Build volume-decorator binary
volume-decorator: generate fmt vet revive
	go build -o output/bin/volume-decorator tkestack.io/volume-decorator/cmd/volume-decorator/

cert-generator: fmt vet revive
	go build -o output/bin/cert-generator tkestack.io/volume-decorator/cmd/cert-generator/

# Run go fmt against code
fmt:
	go fmt ./pkg/... ./cmd/...

# Run go vet against code
vet:
	go vet ./pkg/... ./cmd/...

# Run revive against code
revive:
	go get -u github.com/mgechev/revive
	files=$$(find . -name '*.go' | egrep -v './vendor|generated'); \
	revive -config build/linter/revive.toml -formatter friendly $$files

# Generate code
generate:
	./hack/update-codegen.sh

# Build and push the docker image
image: volume-decorator
	set -ex; \
	cp output/bin/volume-decorator build/docker; \
	docker build -t ${IMAGE_NAME}:latest build/docker; \
	rm build/docker/volume-decorator; \
	for tag in $(IMAGE_TAGS); do \
	  docker tag ${IMAGE_NAME}:latest ${IMAGE_NAME}:$$tag; \
	  docker push ${IMAGE_NAME}:$$tag; \
	done

