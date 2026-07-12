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

WASI_SDK_DIR ?= $(CURDIR)/toolchain/$(WASI_SDK_NAME)
CC := $(WASI_SDK_DIR)/bin/clang

CPY_VERSION     := 3.11
CPY_REF         := v3.11.9
CPY_REPO        := https://github.com/python/cpython
CPY_SRC_DIR     := build/cpython
CPY_WASI_PREFIX := $(CURDIR)/toolchain/wasi-python

OUT := ../../adapter/cpython/cpython.wasm

CFLAGS := --target=wasm32-wasi -O2 \
    -I$(CPY_WASI_PREFIX)/include/python$(CPY_VERSION) \
    -D_GNU_SOURCE -DCONFIG_VERSION='"sango"'

LDFLAGS := -mexec-model=reactor \
    -L$(CPY_WASI_PREFIX)/lib \
    -lpython$(CPY_VERSION) \
    -Wl,--export=allocate \
    -Wl,--export=deallocate \
    -Wl,--export=initialize \
    -Wl,--export=eval \
    -Wl,-z,stack-size=1048576

.PHONY: all install clean distclean

all: $(OUT)

install: $(CC)

$(CC):
	@echo "installing $(WASI_SDK_NAME) ..."
	mkdir -p toolchain
	curl -fL $(WASI_SDK_URL) | tar xz -C toolchain

$(CPY_WASI_PREFIX): $(CC)
	@echo "building WASI CPython from source ..."
	mkdir -p build
	@if [ ! -d $(CPY_SRC_DIR) ]; then \
		git clone --depth 1 --branch $(CPY_REF) $(CPY_REPO) $(CPY_SRC_DIR); \
	fi
	cd $(CPY_SRC_DIR) && \
	CONFIG_SITE=Tools/wasm/config.site-wasm32-wasi \
	CC="$(CC)" \
	CPP="$(WASI_SDK_DIR)/bin/clang-cpp" \
	AR="$(WASI_SDK_DIR)/bin/llvm-ar" \
	RANLIB="$(WASI_SDK_DIR)/bin/llvm-ranlib" \
	./configure \
		--host=wasm32-unknown-wasi \
		--build=$$(./config.guess) \
		--with-build-python=python$(CPY_VERSION) \
		--prefix=$(CPY_WASI_PREFIX) \
		--disable-ipv6
	$(MAKE) -C $(CPY_SRC_DIR)
	$(MAKE) -C $(CPY_SRC_DIR) install

$(OUT): $(CC) $(CPY_WASI_PREFIX) sango_cpython.c
	$(CC) $(CFLAGS) sango_cpython.c -o $(OUT) $(LDFLAGS)
	@ls -lh $(OUT)

clean:
	rm -rf build $(OUT)

distclean: clean
	rm -rf toolchain