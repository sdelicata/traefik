SRCS = $(shell git ls-files '*.go' | grep -v '^vendor/')

TAG_NAME := $(shell git describe --abbrev=0 --tags --exact-match)
SHA := $(shell git rev-parse HEAD)
VERSION_GIT := $(if $(TAG_NAME),$(TAG_NAME),$(SHA))
VERSION := $(if $(VERSION),$(VERSION),$(VERSION_GIT))

BIN_NAME := traefik
CODENAME ?= cheddar

DATE := $(shell date -u '+%Y-%m-%d_%I:%M:%S%p')

# Default build target
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)
GOGC ?=

LINT_EXECUTABLES = misspell shellcheck

DOCKER_BUILD_PLATFORMS ?= linux/amd64,linux/arm64

.PHONY: default
#? default: Run `make generate` and `make binary`
default: generate binary

#? dist: Create the "dist" directory
dist:
	mkdir -p dist

.PHONY: build-webui-image
#? build-webui-image: Build WebUI Docker image
build-webui-image:
	docker build -t traefik-webui -f webui/buildx.Dockerfile webui

.PHONY: clean-webui
#? clean-webui: Clean WebUI static generated assets
clean-webui:
	rm -rf webui/static

webui/static/index.html:
	$(MAKE) build-webui-image
	docker run --rm -v "$(PWD)/webui/static":'/src/webui/static' traefik-webui yarn build:prod
	docker run --rm -v "$(PWD)/webui/static":'/src/webui/static' traefik-webui chown -R $(shell id -u):$(shell id -g) ./static

.PHONY: generate-webui
#? generate-webui: Generate WebUI
generate-webui: webui/static/index.html

.PHONY: generate
#? generate: Generate code (Dynamic and Static configuration documentation reference files)
generate:
	go generate

.PHONY: generate-extproc-proto
#? generate-extproc-proto: Generate ext-proc protobuf stubs from Envoy proto files
generate-extproc-proto:
	@echo "Checking protobuf tools..."
	@which protoc > /dev/null || (echo "Error: protoc not found. Install protoc first." && exit 1)
	@which protoc-gen-go > /dev/null || (echo "Installing protoc-gen-go..." && go install google.golang.org/protobuf/cmd/protoc-gen-go@latest)
	@which protoc-gen-go-grpc > /dev/null || (echo "Installing protoc-gen-go-grpc v1.3.0..." && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.3.0)
	@echo "Creating directory structure..."
	@mkdir -p pkg/proto/envoy/service/ext_proc/v3
	@mkdir -p pkg/proto/envoy/extensions/filters/http/ext_proc/v3
	@mkdir -p pkg/proto/envoy/config/core/v3
	@mkdir -p pkg/proto/envoy/type/v3
	@mkdir -p pkg/proto/google/protobuf
	@echo "Creating minimal proto files for ext-proc..."
	@echo 'syntax = "proto3";\npackage envoy.config.core.v3;\noption go_package = "github.com/traefik/traefik/v3/pkg/proto/envoy/config/core/v3";\nimport "google/protobuf/struct.proto";\nimport "google/protobuf/wrappers.proto";\n\nmessage HeaderValue {\n  string key = 1;\n  string value = 2;\n}\n\nmessage HeaderValueOption {\n  HeaderValue header = 1;\n  google.protobuf.BoolValue append = 2;\n}\n\nmessage HeaderMap {\n  repeated HeaderValue headers = 1;\n}\n\nmessage Metadata {\n  map<string, google.protobuf.Struct> filter_metadata = 1;\n}' > pkg/proto/envoy/config/core/v3/base.proto
	@echo 'syntax = "proto3";\npackage envoy.type.v3;\noption go_package = "github.com/traefik/traefik/v3/pkg/proto/envoy/type/v3";\n\nmessage HttpStatus {\n  uint32 code = 1;\n}' > pkg/proto/envoy/type/v3/http_status.proto
	@echo 'syntax = "proto3";\npackage envoy.extensions.filters.http.ext_proc.v3;\noption go_package = "github.com/traefik/traefik/v3/pkg/proto/envoy/extensions/filters/http/ext_proc/v3";\n\nenum HeaderSendMode {\n  DEFAULT = 0;\n  SEND = 1;\n  SKIP = 2;\n}\n\nenum BodySendMode {\n  NONE = 0;\n  STREAMED = 1;\n  BUFFERED = 2;\n  BUFFERED_PARTIAL = 3;\n  FULL_DUPLEX_STREAMED = 4;\n}\n\nmessage ProcessingMode {\n  HeaderSendMode request_headers_mode = 1;\n  HeaderSendMode response_headers_mode = 2;\n  BodySendMode request_body_mode = 3;\n  BodySendMode response_body_mode = 4;\n  HeaderSendMode request_trailers_mode = 5;\n  HeaderSendMode response_trailers_mode = 6;\n}' > pkg/proto/envoy/extensions/filters/http/ext_proc/v3/processing_mode.proto
	@curl -s -L https://raw.githubusercontent.com/protocolbuffers/protobuf/main/src/google/protobuf/struct.proto -o pkg/proto/google/protobuf/struct.proto
	@curl -s -L https://raw.githubusercontent.com/protocolbuffers/protobuf/main/src/google/protobuf/duration.proto -o pkg/proto/google/protobuf/duration.proto
	@curl -s -L https://raw.githubusercontent.com/protocolbuffers/protobuf/main/src/google/protobuf/wrappers.proto -o pkg/proto/google/protobuf/wrappers.proto
	@echo 'syntax = "proto3";\npackage envoy.service.ext_proc.v3;\noption go_package = "github.com/traefik/traefik/v3/pkg/proto/envoy/service/ext_proc/v3";\n\nimport "envoy/config/core/v3/base.proto";\nimport "envoy/extensions/filters/http/ext_proc/v3/processing_mode.proto";\nimport "envoy/type/v3/http_status.proto";\nimport "google/protobuf/duration.proto";\nimport "google/protobuf/struct.proto";\n\nmessage ProtocolConfiguration {\n  envoy.extensions.filters.http.ext_proc.v3.BodySendMode request_body_mode = 1;\n  envoy.extensions.filters.http.ext_proc.v3.BodySendMode response_body_mode = 2;\n  bool send_body_without_waiting_for_header_response = 3;\n}\n\nmessage ProcessingRequest {\n  oneof request {\n    HttpHeaders request_headers = 1;\n    HttpHeaders response_headers = 2;\n    HttpBody request_body = 3;\n    HttpBody response_body = 4;\n    HttpTrailers request_trailers = 5;\n    HttpTrailers response_trailers = 6;\n  }\n  envoy.config.core.v3.Metadata metadata_context = 8;\n  map<string, google.protobuf.Struct> attributes = 9;\n  bool observability_mode = 10;\n  ProtocolConfiguration protocol_config = 11;\n}\n\nmessage ProcessingResponse {\n  oneof response {\n    HeadersResponse request_headers = 1;\n    HeadersResponse response_headers = 2;\n    BodyResponse request_body = 3;\n    BodyResponse response_body = 4;\n    HeadersResponse request_trailers = 5;\n    HeadersResponse response_trailers = 6;\n    ImmediateResponse immediate_response = 7;\n  }\n  ModeOverride mode_override = 8;\n}\n\nmessage HttpHeaders {\n  envoy.config.core.v3.HeaderMap headers = 1;\n  bool end_of_stream = 2;\n}\n\nmessage HttpBody {\n  bytes body = 1;\n  bool end_of_stream = 2;\n}\n\nmessage HttpTrailers {\n  envoy.config.core.v3.HeaderMap trailers = 1;\n}\n\nmessage HeadersResponse {\n  CommonResponse response = 1;\n}\n\nmessage BodyResponse {\n  CommonResponse response = 1;\n}\n\nmessage CommonResponse {\n  enum ResponseStatus {\n    CONTINUE = 0;\n    CONTINUE_AND_REPLACE = 1;\n  }\n  ResponseStatus status = 1;\n  HeaderMutation header_mutation = 2;\n  BodyMutation body_mutation = 3;\n  google.protobuf.Duration request_timeout = 4;\n}\n\nmessage HeaderMutation {\n  repeated envoy.config.core.v3.HeaderValueOption set_headers = 1;\n  repeated string remove_headers = 2;\n}\n\nmessage BodyMutation {\n  bytes body = 1;\n  bool clear_body = 2;\n}\n\nmessage ImmediateResponse {\n  envoy.type.v3.HttpStatus status = 1;\n  HeaderMutation headers = 2;\n  string body = 3;\n}\n\nmessage ModeOverride {\n  envoy.extensions.filters.http.ext_proc.v3.ProcessingMode processing_mode = 1;\n}\n\nservice ExternalProcessor {\n  rpc Process(stream ProcessingRequest) returns (stream ProcessingResponse);\n}' > pkg/proto/envoy/service/ext_proc/v3/external_processor.proto
	@echo "Generating Go stubs..."
	@cd pkg/proto && protoc \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		envoy/service/ext_proc/v3/external_processor.proto \
		envoy/extensions/filters/http/ext_proc/v3/processing_mode.proto \
		envoy/config/core/v3/base.proto \
		envoy/type/v3/http_status.proto
	@echo "Protobuf stubs generated successfully in pkg/proto/"

.PHONY: binary
#? binary: Build the binary
binary: generate-webui dist
	@echo SHA: $(VERSION) $(CODENAME) $(DATE)
	CGO_ENABLED=0 GOGC=${GOGC} GOOS=${GOOS} GOARCH=${GOARCH} go build ${FLAGS[*]} -ldflags "-s -w \
    -X github.com/traefik/traefik/v3/pkg/version.Version=$(VERSION) \
    -X github.com/traefik/traefik/v3/pkg/version.Codename=$(CODENAME) \
    -X github.com/traefik/traefik/v3/pkg/version.BuildDate=$(DATE)" \
    -installsuffix nocgo -o "./dist/${GOOS}/${GOARCH}/$(BIN_NAME)" ./cmd/traefik

binary-linux-arm64: export GOOS := linux
binary-linux-arm64: export GOARCH := arm64
binary-linux-arm64:
	@$(MAKE) binary

binary-linux-amd64: export GOOS := linux
binary-linux-amd64: export GOARCH := amd64
binary-linux-amd64:
	@$(MAKE) binary

binary-windows-amd64: export GOOS := windows
binary-windows-amd64: export GOARCH := amd64
binary-windows-amd64: export BIN_NAME := traefik.exe
binary-windows-amd64:
	@$(MAKE) binary

.PHONY: crossbinary-default
#? crossbinary-default: Build the binary for the standard platforms (linux, darwin, windows)
crossbinary-default: generate generate-webui
	$(CURDIR)/script/crossbinary-default.sh

.PHONY: test
#? test: Run the unit and integration tests
test: test-ui-unit test-unit test-integration

.PHONY: test-unit
#? test-unit: Run the unit tests
test-unit:
	GOOS=$(GOOS) GOARCH=$(GOARCH) go test -cover "-coverprofile=cover.out" -v $(TESTFLAGS) ./pkg/... ./cmd/...

.PHONY: test-integration
#? test-integration: Run the integration tests
test-integration:
	GOOS=$(GOOS) GOARCH=$(GOARCH) go test ./integration -test.timeout=20m -failfast -v $(TESTFLAGS)

.PHONY: test-gateway-api-conformance
#? test-gateway-api-conformance: Run the conformance tests
test-gateway-api-conformance: build-image-dirty
	# In case of a new Minor/Major version, the k8sConformanceTraefikVersion needs to be updated.
	GOOS=$(GOOS) GOARCH=$(GOARCH) go test ./integration -v -test.run K8sConformanceSuite -k8sConformance -k8sConformanceTraefikVersion="v3.5" $(TESTFLAGS)

.PHONY: test-ui-unit
#? test-ui-unit: Run the unit tests for the webui
test-ui-unit:
	$(MAKE) build-webui-image
	docker run --rm -v "$(PWD)/webui/static":'/src/webui/static' traefik-webui yarn --cwd webui install
	docker run --rm -v "$(PWD)/webui/static":'/src/webui/static' traefik-webui yarn --cwd webui test:unit:ci

.PHONY: pull-images
#? pull-images: Pull all Docker images to avoid timeout during integration tests
pull-images:
	grep --no-filename -E '^\s+image:' ./integration/resources/compose/*.yml \
		| awk '{print $$2}' \
		| sort \
		| uniq \
		| xargs -P 6 -n 1 docker pull

.PHONY: lint
#? lint: Run golangci-lint
lint:
	golangci-lint run

.PHONY: validate-files
#? validate-files: Validate code and docs
validate-files:
	$(foreach exec,$(LINT_EXECUTABLES),\
            $(if $(shell which $(exec)),,$(error "No $(exec) in PATH")))
	$(CURDIR)/script/validate-vendor.sh
	$(CURDIR)/script/validate-misspell.sh
	$(CURDIR)/script/validate-shell-script.sh

.PHONY: validate
#? validate: Validate code, docs, and vendor
validate: lint validate-files

# Target for building images for multiple architectures.
.PHONY: multi-arch-image-%
multi-arch-image-%: binary-linux-amd64 binary-linux-arm64
	docker buildx build $(DOCKER_BUILDX_ARGS) -t traefik/traefik:$* --platform=$(DOCKER_BUILD_PLATFORMS) -f Dockerfile .


.PHONY: build-image
#? build-image: Clean up static directory and build a Docker Traefik image
build-image: export DOCKER_BUILDX_ARGS := --load
build-image: export DOCKER_BUILD_PLATFORMS := linux/$(GOARCH)
build-image: clean-webui
	@$(MAKE) multi-arch-image-latest

.PHONY: build-image-dirty
#? build-image-dirty: Build a Docker Traefik image without re-building the webui when it's already built
build-image-dirty: export DOCKER_BUILDX_ARGS := --load
build-image-dirty: export DOCKER_BUILD_PLATFORMS := linux/$(GOARCH)
build-image-dirty:
	@$(MAKE) multi-arch-image-latest

.PHONY: docs
#? docs: Build documentation site
docs:
	make -C ./docs docs

.PHONY: docs-serve
#? docs-serve: Serve the documentation site locally
docs-serve:
	make -C ./docs docs-serve

.PHONY: docs-pull-images
#? docs-pull-images: Pull image for doc building
docs-pull-images:
	make -C ./docs docs-pull-images

.PHONY: generate-crd
#? generate-crd: Generate CRD clientset and CRD manifests
generate-crd:
	@$(CURDIR)/script/code-gen.sh

.PHONY: generate-genconf
#? generate-genconf: Generate code from dynamic configuration github.com/traefik/genconf
generate-genconf:
	go run ./cmd/internal/gen/

.PHONY: release-packages
#? release-packages: Create packages for the release
release-packages: generate-webui
	$(CURDIR)/script/release-packages.sh

.PHONY: fmt
#? fmt: Format the Code
fmt:
	gofmt -s -l -w $(SRCS)

.PHONY: help
#? help: Get more info on make commands
help: Makefile
	@echo " Choose a command run in traefik:"
	@sed -n 's/^#?//p' $< | column -t -s ':' |  sort | sed -e 's/^/ /'
