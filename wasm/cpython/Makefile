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

HOST_PYTHON ?= python3.13

CPY_VERSION  := 3.13
CPY_REF      := v3.13.5
CPY_REPO     := https://github.com/python/cpython
CPY_SRC_DIR  := build/cpython
CPY_INSTALL  := $(CPY_SRC_DIR)/cross-build/wasm32-wasip1/install
CPY_MPDEC := $(CPY_SRC_DIR)/cross-build/wasm32-wasip1/Modules/_decimal/libmpdec
CPY_EXPAT := $(CPY_SRC_DIR)/cross-build/wasm32-wasip1/Modules/expat
CPY_HACL  := $(CPY_SRC_DIR)/cross-build/wasm32-wasip1/Modules/_hacl
HACL_OBJS := $(wildcard $(CPY_HACL)/Hacl_Hash_SHA2.o \
                        $(CPY_HACL)/Hacl_Hash_SHA1.o \
                        $(CPY_HACL)/Hacl_Hash_MD5.o \
                        $(CPY_HACL)/Hacl_Hash_SHA3.o \
                        $(CPY_HACL)/Hacl_Hash_Blake2b.o \
                        $(CPY_HACL)/Hacl_Hash_Blake2s.o \
                        $(CPY_HACL)/Hacl_Hash_Blake2b_Simd256.o \
                        $(CPY_HACL)/Hacl_Hash_Blake2s_Simd128.o)

OUT        := ../../adapter/cpython/cpython.wasm
STDLIB_ZIP := ../../adapter/cpython/python$(CPY_VERSION).zip

CFLAGS := --target=wasm32-wasip1 -O2 \
    -ffunction-sections -fdata-sections \
    -D_WASI_EMULATED_SIGNAL \
    -D_WASI_EMULATED_GETPID \
    -D_WASI_EMULATED_PROCESS_CLOCKS \
    -D_WASI_EMULATED_MMAN \
    -I$(CPY_INSTALL)/include/python$(CPY_VERSION) \
    -D_GNU_SOURCE

LDFLAGS := -mexec-model=reactor \
    -L$(CPY_INSTALL)/lib \
    -L$(CPY_MPDEC) \
    -L$(CPY_EXPAT) \
    -lpython$(CPY_VERSION) \
    -lmpdec \
    -lexpat \
    -lwasi-emulated-signal \
    -lwasi-emulated-getpid \
    -lwasi-emulated-process-clocks \
    -lwasi-emulated-mman \
    -ldl \
    -Wl,--gc-sections \
    -Wl,--export=allocate \
    -Wl,--export=deallocate \
    -Wl,--export=initialize \
    -Wl,--export=eval \
    -Wl,-z,stack-size=8388608

.PHONY: all install clean distclean stdlib

all: $(OUT) $(STDLIB_ZIP)

install: $(CC)

$(CC):
	@echo "installing $(WASI_SDK_NAME) ..."
	mkdir -p toolchain
	curl -fL $(WASI_SDK_URL) | tar xz -C toolchain

$(CPY_SRC_DIR):
	mkdir -p build
	git clone --depth 1 --branch $(CPY_REF) $(CPY_REPO) $(CPY_SRC_DIR)

$(CPY_INSTALL):	$(CC) $(CPY_SRC_DIR)
	@echo "building CPython for wasm32-wasip1 via Tools/wasm/wasi.py ..."
	cd $(CPY_SRC_DIR) && \
	  WASI_SDK_PATH=$(WASI_SDK_DIR) \
      $(HOST_PYTHON) Tools/wasm/wasi.py build
	$(MAKE) -C $(CPY_SRC_DIR)/cross-build/wasm32-wasip1 install \
      prefix=$(abspath $(CPY_INSTALL))
	@test -f $(CPY_INSTALL)/lib/libpython$(CPY_VERSION).a || \
	  { echo "ERROR: libpython$(CPY_VERSION).a not found under $(CPY_INSTALL)/lib"; exit 1; }

$(OUT):	$(CC) $(CPY_INSTALL) sango_cpython.c
	$(CC) $(CFLAGS) sango_cpython.c $(HACL_OBJS) -o $(OUT) $(LDFLAGS)
	@ls -lh $(OUT)

stdlib: $(STDLIB_ZIP)

$(STDLIB_ZIP): $(CPY_INSTALL)
	@echo "packaging stdlib into $(STDLIB_ZIP) ..."
	cd $(CPY_INSTALL)/lib/python$(CPY_VERSION) && \
		zip -qr $(abspath $(STDLIB_ZIP)) . \
			-x "test/*" "*/test/*" "*/tests/*" \
			"*/__pycache__/*" "*.pyc" \
			"idlelib/*" "tkinter/*" "turtledemo/*" \
			"ensurepip/*" "pydoc_data/*"

clean:
	rm -rf build $(OUT) $(STDLIB_ZIP)

distclean: clean
	rm -rf toolchain