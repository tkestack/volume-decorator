
all: test volume-manager

# Run tests
test: generate fmt vet revive 
	go test ./pkg/... ./cmd/... -coverprofile cover.out

# Build volume-manager binary
volume-manager: generate fmt vet revive
	go build -o output/bin/volume-manager tkestack.io/volume-decorator/cmd/volume-manager/

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
	files=$$(find . -name '*.go' | egrep -v './vendor|generated'); \
	revive -config build/linter/revive.toml -formatter friendly $$files

# Generate code
generate:
	./hack/update-codegen.sh

# Build the docker image
volume-manager-image: volume-manager
	cp output/bin/volume-manager build/docker
	docker build --network=host -t volume-manager:latest build/docker
	rm build/docker/volume-manager

