# Guest-side tools for firecracker-vm://image (see docs/DESIGN.md).
#   make -C cri/firecracker fcinit     static guest init -> $(CACHE)/fcinit
#   make -C cri/firecracker fc-rootfs  pre-build CLI     -> ../../bin/fc-rootfs
CACHE ?= /users/Arun1903/fc-vm/images
BIN   ?= $(abspath ../../bin)

.PHONY: fcinit fc-rootfs all
all: fcinit fc-rootfs

fcinit:
	mkdir -p $(CACHE)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o $(CACHE)/fcinit.new ./cmd/fcinit
	mv $(CACHE)/fcinit.new $(CACHE)/fcinit

fc-rootfs:
	mkdir -p $(BIN)
	go build -o $(BIN)/fc-rootfs ./cmd/fc-rootfs
