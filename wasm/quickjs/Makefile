WASI_SDK_VERSION ?= 33

UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

ifeq ($(UNAME_S),Darwin)
  WASI_OS := macos
else
  WASI_OS := linux
endif

ifeq ($(filter $(UNAME_M),arm64 aarch64),)
  WASI_ARCH := x86_64
else
  WASI_ARCH := arm64
endif

WASI_SDK_NAME := wasi-sdk-$(WASI_SDK_VERSION).0-$(WASI_ARCH)-$(WASI_OS)
WASI_SDK_URL  := https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-$(WASI_SDK_VERSION)/$(WASI_SDK_NAME).tar.gz

WASI_SDK ?= toolchain/$(WASI_SDK_NAME)
CC := $(WASI_SDK)/bin/clang

QJS_REPO ?= https://github.com/quickjs-ng/quickjs
QJS_REF  ?= v0.10.1
QJS_DIR  := build/quickjs
OUT      := ../../adapter/quickjs/quickjs.wasm

CFLAGS := --target=wasm32-wasi -O2 -I$(QJS_DIR) \
	-D_GNU_SOURCE -DCONFIG_VERSION='"sango"'

LDFLAGS := -mexec-model=reactor \
	-Wl,--export=allocate \
	-Wl,--export=deallocate \
	-Wl,--export=initialize \
	-Wl,--export=eval \
	-Wl,-z,stack-size=1048576

QJS_SRCS := $(QJS_DIR)/quickjs.c \
	$(QJS_DIR)/cutils.c \
	$(QJS_DIR)/libregexp.c \
	$(QJS_DIR)/libunicode.c \
	$(QJS_DIR)/xsum.c

.PHONY: all install clean check-toolchain

all: $(OUT)

install: $(CC)

$(CC):
	@echo "installing $(WASI_SDK_NAME) ..."
	mkdir -p toolchain
	curl -fL $(WASI_SDK_URL) | tar xz -C toolchain

WASI_SDK ?= /opt/wasi-sdk
QJS_REPO ?= https://github.com/quickjs-ng/quickjs
QJS_REF  ?= v0.10.1
QJS_DIR  := build/quickjs
OUT      := ../../adapter/quickjs/quickjs.wasm

CC := $(WASI_SDK)/clang

CFLAGS := --target=wasm32-wasi -O2 -I$(QJS_DIR) \
	-D_GNU_SOURCE -DCONFIG_VERSION='"sango"'

LDFLAGS := -mexec-model=reactor \
	-Wl,--export=allocate \
	-Wl,--export=deallocate \
	-Wl,--export=initialize \
	-Wl,--export=eval \
	-Wl,-z,stack-size=1048576

QJS_SRCS := $(QJS_DIR)/quickjs.c \
	$(QJS_DIR)/cutils.c \
	$(QJS_DIR)/libregexp.c \
	$(QJS_DIR)/libunicode.c \
	$(QJS_DIR)/xsum.c

.PHONY: all clean check-toolchain

all: $(OUT)

install: $(CC)

$(CC):
	@echo "installing $(WASI_SDK_NAME) ..."
	mkdir -p toolchain
	curl -fL $(WASI_SDK_URL) | tar xz -C toolchain

check-toolchain:
	@test -x $(CC) || { \
		echo "error: not found wasi-sdk ($(CC))"; \
		echo "Please set WASI_SDK according to the instructions at the beginning of this Makefile"; \
		exit 1; }

$(OUT): check-toolchain $(QJS_DIR) sango_quickjs.c
	$(CC) $(CFLAGS) $(LDFLAGS) sango_quickjs.c $(QJS_SRCS) -o $(OUT)
	@ls -lh $(OUT)

$(QJS_DIR):
	git clone --depth 1 --branch $(QJS_REF) $(QJS_REPO) $(QJS_DIR)
 
clean:
	rm -rf build $(OUT)

distclean: clean
	rm -rf toolchain
