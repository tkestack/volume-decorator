#!/bin/bash

# Copyright 2019 THL A29 Limited, a Tencent company.

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# 	http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname ${BASH_SOURCE})/..
CODEGEN_PKG=${CODEGEN_PKG:-$(go list -f '{{ .Dir }}' -m k8s.io/code-generator)}
TMP_CODEGEN_PKG="./tmp-code-generator"
cp -R ${CODEGEN_PKG} ${TMP_CODEGEN_PKG}

function cleanup() {
    sudo rm -rf ${TMP_CODEGEN_PKG}
}

trap cleanup EXIT SIGINT SIGTERM

chmod +x ${TMP_CODEGEN_PKG}/generate-groups.sh
${TMP_CODEGEN_PKG}/generate-groups.sh all \
  tkestack.io/volume-decorator/pkg/generated tkestack.io/volume-decorator/pkg/apis \
  storage:v1 \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt
