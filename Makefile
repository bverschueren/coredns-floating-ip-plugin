MKFILE_DIR := $(dir $(abspath $(firstword $(MAKEFILE_LIST))))
BUILDOPTS?=-v
BUILD_DIR?=build
BINARY?=$(BUILD_DIR)/coredns
COREDNS_SRC_DIR := $(BUILD_DIR)/coredns-src
SYSTEM:=
LINUX_ARCH:=amd64 arm arm64 mips64le ppc64le s390x mips riscv64
VERSION := $(shell git describe --abbrev=0 --tags)

all: test coredns

.PHONY: test
test: fmt vet
	go test -v ./...

.PHONY: fmt
fmt:
	go fmt -mod=mod *.go
	git diff --exit-code

.PHONY: vet
vet:
	go vet *.go

.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)
	rm -rf release/

.PHONY: clean-bin
clean-bin:
	rm -f $(BINARY)

.PHONY: clean-release
clean-release:
	rm -rf $(BUILD_DIR)/darwin $(BUILD_DIR)/linux $(BUILD_DIR)/windows

$(COREDNS_SRC_DIR):
	mkdir -p $(BUILD_DIR)
	@echo preparing coredns sources in $(COREDNS_SRC_DIR)
	git clone https://github.com/coredns/coredns $(COREDNS_SRC_DIR)
	@echo updating go.mod to use local sources
	cd $(COREDNS_SRC_DIR) && \
		go mod edit -replace github.com/bverschueren/coredns-floating-ip-plugin=$(MKFILE_DIR) && \
		echo ospfip:github.com/bverschueren/coredns-floating-ip-plugin >> ./plugin.cfg

.PHONY: coredns-deps
coredns-deps: $(COREDNS_SRC_DIR)
	cd $(COREDNS_SRC_DIR) && \
		go get -u github.com/bverschueren/coredns-floating-ip-plugin && \
		go get && \
		go generate

coredns: clean-bin coredns-deps
	cd $(COREDNS_SRC_DIR) && \
		$(SYSTEM) go build $(BUILDOPTS) -o $(MKFILE_DIR)/$(BINARY)

.PHONY: build
build: clean-release coredns-deps
	@echo Building: darwin/amd64 - $(VERSION)
	mkdir -p $(BUILD_DIR)/darwin/amd64 && $(MAKE) coredns BINARY=$(BUILD_DIR)/darwin/amd64/coredns SYSTEM="GOOS=darwin GOARCH=amd64"
	@echo Building: darwin/arm64 - $(VERSION)
	mkdir -p $(BUILD_DIR)/darwin/arm64 && $(MAKE) coredns BINARY=$(BUILD_DIR)/darwin/arm64/coredns SYSTEM="GOOS=darwin GOARCH=arm64"
	@echo Building: windows/amd64 - $(VERSION)
	mkdir -p $(BUILD_DIR)/windows/amd64 && $(MAKE) coredns BINARY=$(BUILD_DIR)/windows/amd64/coredns.exe SYSTEM="GOOS=windows GOARCH=amd64"
	@echo Building: linux/$(LINUX_ARCH) - $(VERSION) ;\
	for arch in $(LINUX_ARCH); do \
	    mkdir -p $(BUILD_DIR)/linux/$$arch  && $(MAKE) coredns BINARY=$(BUILD_DIR)/linux/$$arch/coredns SYSTEM="GOOS=linux GOARCH=$$arch" ;\
	done

.PHONY: release
release: build
	@echo Cleaning old releases
	@rm -rf release && mkdir release
	tar -zcf release/coredns_$(VERSION)_darwin_amd64.tgz -C $(BUILD_DIR)/darwin/amd64 coredns
	tar -zcf release/coredns_$(VERSION)_darwin_arm64.tgz -C $(BUILD_DIR)/darwin/arm64 coredns
	tar -zcf release/coredns_$(VERSION)_windows_amd64.tgz -C $(BUILD_DIR)/windows/amd64 coredns.exe
	for arch in $(LINUX_ARCH); do \
	    tar -zcf release/coredns_$(VERSION)_linux_$$arch.tgz -C $(BUILD_DIR)/linux/$$arch coredns ;\
	done
