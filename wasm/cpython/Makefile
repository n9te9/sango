.DEFAULT_GOAL := all

UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

WASI_SDK_VERSION ?= 33
 
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
WASI_SDK_DIR  ?= $(CURDIR)/toolchain/$(WASI_SDK_NAME)
CC            := $(WASI_SDK_DIR)/bin/clang
CXX := $(WASI_SDK_DIR)/bin/clang++

PBS_TAG   ?= 20260718
PBS_PYVER ?= 3.13.14
 
ifeq ($(UNAME_S),Darwin)
  PBS_PLAT := apple-darwin
else
  PBS_PLAT := unknown-linux-gnu
endif
ifeq ($(filter $(UNAME_M),arm64 aarch64),)
  PBS_ARCH := x86_64
else
  PBS_ARCH := aarch64
endif

PBS_NAME := cpython-$(PBS_PYVER)+$(PBS_TAG)-$(PBS_ARCH)-$(PBS_PLAT)-install_only
PBS_URL  := https://github.com/astral-sh/python-build-standalone/releases/download/$(PBS_TAG)/$(PBS_NAME).tar.gz

HOST_PY     := $(CURDIR)/toolchain/python/bin/python3.13
VENV        := $(CURDIR)/build/venv

HOST_PYTHON := $(VENV)/bin/python

CPY_VERSION := 3.13
CPY_REF     := v3.13.5
CPY_REPO    := https://github.com/python/cpython
CPY_SRC_DIR := build/cpython
CPY_INSTALL := $(CPY_SRC_DIR)/cross-build/wasm32-wasip1/install
CPY_MPDEC   := $(CPY_SRC_DIR)/cross-build/wasm32-wasip1/Modules/_decimal/libmpdec
CPY_EXPAT   := $(CPY_SRC_DIR)/cross-build/wasm32-wasip1/Modules/expat
CPY_HACL    := $(CPY_SRC_DIR)/cross-build/wasm32-wasip1/Modules/_hacl
HACL_OBJS := $(wildcard $(CPY_HACL)/Hacl_Hash_SHA2.o \
                        $(CPY_HACL)/Hacl_Hash_SHA1.o \
                        $(CPY_HACL)/Hacl_Hash_MD5.o \
                        $(CPY_HACL)/Hacl_Hash_SHA3.o \
                        $(CPY_HACL)/Hacl_Hash_Blake2b.o \
                        $(CPY_HACL)/Hacl_Hash_Blake2s.o \
                        $(CPY_HACL)/Hacl_Hash_Blake2b_Simd256.o \
                        $(CPY_HACL)/Hacl_Hash_Blake2s_Simd128.o)

ZLIB_VERSION := 1.3.1
ZLIB_SRC     := build/zlib-$(ZLIB_VERSION)
ZLIB_TAR     := build/zlib-$(ZLIB_VERSION).tar.gz
ZLIB_INSTALL := $(CURDIR)/build/zlib-install
ZLIB_URL     := https://zlib.net/fossils/zlib-$(ZLIB_VERSION).tar.gz

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
    -fwasm-exceptions \
    -L$(WASI_SDK_DIR)/share/wasi-sysroot/lib/wasm32-wasip1/eh \
    -L$(CPY_INSTALL)/lib \
    -L$(CPY_MPDEC) \
    -L$(CPY_EXPAT) \
    -L$(ZLIB_INSTALL)/lib \
    -lpython$(CPY_VERSION) \
    -lmpdec \
    -lexpat \
    -lz \
    -lc++abi \
    -lunwind \
    -lwasi-emulated-signal \
    -lwasi-emulated-getpid \
    -lwasi-emulated-process-clocks \
    -lwasi-emulated-mman \
    -ldl \
    -Wl,--strip-debug \
    -Wl,--gc-sections \
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
		-Wl,--strip-debug \
    -Wl,--gc-sections \
    -Wl,--export=allocate \
    -Wl,--export=deallocate \
    -Wl,--export=initialize \
	-Wl,--allow-multiple-definition \
    -Wl,--export=eval \
		-Wl,--wrap=strtold \
		-Wl,--wrap=strtold_l \
    -Wl,-z,stack-size=8388608

_PYTHON_SYSCONFIGDATA_NAME=_sysconfigdata__wasi_wasm32-wasi
PYTHONPATH=$(CPY_INSTALL)/lib/python3.13 

# numpy cross build

MS_VERSION := 3.0.2
MS_SDIST   := build/sdists/markupsafe-$(MS_VERSION).tar.gz
MS_SRC     := build/sdists/markupsafe-$(MS_VERSION)
MS_OBJ     := build/ext/markupsafe_speedups.o

$(MS_SDIST): $(HOST_PYTHON)
	mkdir -p build/sdists
	$(VENV)/bin/pip download markupsafe==$(MS_VERSION) \
	  --no-binary :all: --no-deps -d build/sdists

$(MS_OBJ): $(CC) $(CPY_INSTALL) $(MS_SRC)
	mkdir -p build/ext
	$(CC) $(CFLAGS) -c $(MS_SRC)/src/markupsafe/_speedups.c -o $(MS_OBJ)

NUMPY_VERSION := 2.1.3
NUMPY_SRC     := build/numpy
CROSS_INI     := build/wasi-cross.ini

$(CROSS_INI): Makefile
	mkdir -p build
	@printf '%s\n' \
	  '[binaries]' \
	  "c = '$(WASI_SDK_DIR)/bin/clang'" \
    "cpp = '$(WASI_SDK_DIR)/bin/clang++'" \
	  "ar = '$(WASI_SDK_DIR)/bin/llvm-ar'" \
	  "strip = '$(WASI_SDK_DIR)/bin/llvm-strip'" \
	  "python = '$(abspath $(HOST_PYTHON))'" \
	  '' \
	  '[host_machine]' \
	  "system = 'wasi'" \
	  "cpu_family = 'wasm32'" \
	  "cpu = 'wasm32'" \
	  "endian = 'little'" \
	  '' \
    '[properties]' \
	  "longdouble_format = 'IEEE_DOUBLE_LE'" \
	  "python_purelib = '$(abspath $(CPY_INSTALL))/lib/python$(CPY_VERSION)'" \
	  "python_platlib = '$(abspath $(CPY_INSTALL))/lib/python$(CPY_VERSION)'" \
	  '' \
	  '[built-in options]' \
	  "c_args = ['--target=wasm32-wasip1', '-D_WASI_EMULATED_SIGNAL', '-D_WASI_EMULATED_MMAN', '-D__EMSCRIPTEN__', '-DCYTHON_COMPRESS_STRINGS=0', '-Wno-incompatible-pointer-types', '-I$(abspath $(CPY_INSTALL))/include/python$(CPY_VERSION)']" \
	  "c_link_args = ['--target=wasm32-wasip1']" \
	  "cpp_args = ['--target=wasm32-wasip1', '-D_WASI_EMULATED_SIGNAL', '-D_WASI_EMULATED_MMAN', '-fwasm-exceptions', '-D__EMSCRIPTEN__', '-DCYTHON_COMPRESS_STRINGS=0', '-I$(abspath $(CPY_INSTALL))/include/python$(CPY_VERSION)']" \
	  "cpp_link_args = ['--target=wasm32-wasip1', '-fwasm-exceptions']" \
	  > $(CROSS_INI)
	@echo "generated $(CROSS_INI)"

NUMPY_BUILDDIR := $(NUMPY_SRC)/builddir-wasi

$(NUMPY_BUILDDIR)/build.ninja: $(CROSS_INI) $(NUMPY_SRC) $(HOST_PYTHON)
	cd $(NUMPY_SRC) && \
	  PATH="$(abspath $(VENV))/bin:$$PATH" \
	  _PYTHON_SYSCONFIGDATA_NAME=_sysconfigdata__wasi_wasm32-wasi \
	  PYTHONPATH="$(abspath $(CPY_INSTALL))/lib/python$(CPY_VERSION)" \
	  $(abspath $(HOST_PYTHON)) vendored-meson/meson/meson.py setup builddir-wasi \
	    --cross-file $(abspath $(CROSS_INI)) \
	    -Dblas=none -Dlapack=none -Ddisable-optimization=true

NUMPY_UMATH_DIR := $(NUMPY_BUILDDIR)/numpy/_core/_multiarray_umath.cpython-313-wasm32-wasi.so.p
NUMPY_LINALG_DIR := $(NUMPY_BUILDDIR)/numpy/linalg/_umath_linalg.cpython-313-wasm32-wasi.so.p

NUMPY_INSTALL := build/numpy-install

# pandas cross build

PANDAS_VERSION   := 2.3.3
PANDAS_SRC       := build/pandas
PANDAS_BUILDDIR  := $(PANDAS_SRC)/builddir-wasi
PANDAS_CROSS_INI := build/wasi-cross-pandas.ini

NUMPY_INC        := build/numpy-inc
NUMPY_INC_STAMP   := $(NUMPY_INC)/numpy/_numpyconfig.h
NUMPY_GEN_HEADERS := __multiarray_api.h __ufunc_api.h

VENV_SITE        := $(VENV)/lib/python$(CPY_VERSION)/site-packages
VENV_PANDAS_DEPS := $(VENV)/.stamp-pandas-deps
VENV_NUMPY_WASI  := $(VENV)/.stamp-numpy-headers-wasi

PY_DEPS_DIR := build/pydeps

SHIM_DIR := shims
SHIM_SRCS := $(wildcard $(SHIM_DIR)/*.py)

PANDAS_PATCH_STAMP := build/.stamp-pandas-patched

.PHONY: all toolchains install stdlib clean distclean ext-markupsafe numpy-configure numpy-compile numpy-objs numpy-headers pandas-configure pandas-compile pandas-verify pandas-symbols

ext-markupsafe: $(MS_OBJ)
	@echo "markupsafe _speedups.o built: $(MS_OBJ)"

$(MS_SRC): $(MS_SDIST)
	cd build/sdists && tar xzf markupsafe-$(MS_VERSION).tar.gz
	@test -f $(MS_SRC)/src/markupsafe/_speedups.c || \
	  { echo "ERROR: _speedups.c not found. check: ls build/sdists/"; exit 1; }

$(ZLIB_TAR):
	mkdir -p build
	curl -fL $(ZLIB_URL) -o $@

$(ZLIB_SRC): $(ZLIB_TAR)
	tar xzf $(ZLIB_TAR) -C build

$(ZLIB_INSTALL)/lib/libz.a: $(ZLIB_SRC) $(CC)
	@echo "building zlib for wasm32-wasip1 ..."
	cd $(ZLIB_SRC) && \
	  CC="$(CC)" \
	  CFLAGS="--target=wasm32-wasip1 -O2 -D_WASI_EMULATED_SIGNAL" \
	  ./configure --static --prefix=$(ZLIB_INSTALL)
	$(MAKE) -C $(ZLIB_SRC) install \
	  AR="$(WASI_SDK_DIR)/bin/llvm-ar" \
	  ARFLAGS="rc" \
	  RANLIB="$(WASI_SDK_DIR)/bin/llvm-ranlib"

$(PY_DEPS_DIR): $(HOST_PYTHON)
	rm -rf $(PY_DEPS_DIR)
	$(VENV)/bin/pip install --no-compile --target $(PY_DEPS_DIR) \
	  python-dateutil pytz tzdata
	@test -d $(PY_DEPS_DIR)/dateutil || { echo "ERROR: dateutil missing"; exit 1; }
	@test -d $(PY_DEPS_DIR)/tzdata   || { echo "ERROR: tzdata missing"; exit 1; }

$(NUMPY_INC_STAMP): $(NUMPY_BUILDDIR)/build.ninja
	rm -rf $(NUMPY_INC)
	mkdir -p $(NUMPY_INC)/numpy
	cp -R $(NUMPY_SRC)/numpy/_core/include/numpy/. $(NUMPY_INC)/numpy/
	@src=$$(find $(NUMPY_BUILDDIR) -name '_numpyconfig.h' | head -1); \
	 if [ -z "$$src" ]; then \
	   echo "ERROR: _numpyconfig.h not found under $(NUMPY_BUILDDIR)"; \
	   echo "  try: make numpy-configure  (meson setup --reconfigure)"; \
	   exit 1; \
	 fi; \
	 cp "$$src" $(NUMPY_INC)/numpy/; \
	 echo "  + _numpyconfig.h  <- $$src"
	 @for h in $(NUMPY_GEN_HEADERS); do \
	   src=$$(find $(NUMPY_BUILDDIR) -name "$$h" | head -1); \
	   if [ -z "$$src" ]; then \
	     echo "ERROR: $$h not found.:"; \
	     echo "  make numpy-compile"; \
	     exit 1; \
	   fi; \
	   cp "$$src" $(NUMPY_INC)/numpy/; \
	   echo "  + $$h  <- $$src"; \
	 done
	@grep -qE 'NPY_SIZEOF_PY_INTPTR_T[[:space:]]+4' $(NUMPY_INC_STAMP) || { \
	   echo "ERROR: not a wasm32 numpy config:"; \
	   grep NPY_SIZEOF $(NUMPY_INC_STAMP); exit 1; }
	@echo "numpy wasm32 headers ready: $(NUMPY_INC)"

$(VENV_PANDAS_DEPS): $(HOST_PYTHON)
	$(VENV)/bin/pip install "numpy==$(NUMPY_VERSION)" "cython~=3.0.5" "versioneer[toml]"
	touch $@

$(NUMPY_SRC):
	mkdir -p build 
	git clone --depth 1 --branch v$(NUMPY_VERSION) https://github.com/numpy/numpy $(NUMPY_SRC)
	cd $(NUMPY_SRC) && git submodule update --init --recursive

$(VENV_NUMPY_WASI): $(VENV_PANDAS_DEPS) $(NUMPY_INC_STAMP)
	@d=$(VENV_SITE)/numpy/_core/include/numpy; \
	  test -d $$d || { echo "ERROR: host numpy include dir not found: $$d"; exit 1; }; \
	  cp $(NUMPY_INC)/numpy/*.h $$d/; \
	  grep -qE 'NPY_SIZEOF_PY_INTPTR_T[[:space:]]+4' $$d/_numpyconfig.h || { \
	    echo "ERROR: header swap failed"; exit 1; }; \
	  echo "swapped: $$d"
	touch $@

$(PANDAS_CROSS_INI): Makefile
	mkdir -p build
	@printf '%s\n' \
	  '[binaries]' \
	  "c = '$(WASI_SDK_DIR)/bin/clang'" \
	  "cpp = '$(WASI_SDK_DIR)/bin/clang++'" \
	  "ar = '$(WASI_SDK_DIR)/bin/llvm-ar'" \
	  "strip = '$(WASI_SDK_DIR)/bin/llvm-strip'" \
	  '' \
	  '[host_machine]' \
	  "system = 'wasi'" \
	  "cpu_family = 'wasm32'" \
	  "cpu = 'wasm32'" \
	  "endian = 'little'" \
	  '' \
	  '[properties]' \
	  "longdouble_format = 'IEEE_DOUBLE_LE'" \
	  "numpy-include-dir = '$(abspath $(NUMPY_INC))'" \
	  "python_purelib = '$(abspath $(CPY_INSTALL))/lib/python$(CPY_VERSION)'" \
	  "python_platlib = '$(abspath $(CPY_INSTALL))/lib/python$(CPY_VERSION)'" \
	  '' \
	  '[built-in options]' \
	  "c_args = ['--target=wasm32-wasip1', '-D_WASI_EMULATED_SIGNAL', '-D_WASI_EMULATED_MMAN', '-D__EMSCRIPTEN__', '-Wno-incompatible-pointer-types', '-DCYTHON_COMPRESS_STRINGS=0', '-I$(abspath $(NUMPY_INC))', '-I$(abspath $(CPY_INSTALL))/include/python$(CPY_VERSION)']" \
	  "c_link_args = ['--target=wasm32-wasip1', '-Wl,--allow-undefined']" \
	  "cpp_args = ['--target=wasm32-wasip1', '-D_WASI_EMULATED_SIGNAL', '-D_WASI_EMULATED_MMAN', '-fwasm-exceptions', '-D__EMSCRIPTEN__', '-DCYTHON_COMPRESS_STRINGS=0', '-I$(abspath $(NUMPY_INC))', '-I$(abspath $(CPY_INSTALL))/include/python$(CPY_VERSION)']" \
	  "cpp_link_args = ['--target=wasm32-wasip1', '-fwasm-exceptions', '-Wl,--allow-undefined', '-Wl,--no-entry', '-Wl,--export-all', '-nostdlib++']" \
	  > $(PANDAS_CROSS_INI)
	@echo "generated $(PANDAS_CROSS_INI)"

$(PANDAS_SRC):
	mkdir -p build
	git clone --depth 1 --branch v$(PANDAS_VERSION) \
	  https://github.com/pandas-dev/pandas $(PANDAS_SRC)

$(PANDAS_BUILDDIR)/build.ninja: $(PANDAS_CROSS_INI) $(PANDAS_SRC) $(VENV_NUMPY_WASI) $(PANDAS_PATCH_STAMP)
	cd $(PANDAS_SRC) && \
	  PATH="$(abspath $(VENV))/bin:$$PATH" \
	  _PYTHON_SYSCONFIGDATA_NAME=_sysconfigdata__wasi_wasm32-wasi \
	  PYTHONPATH="$(abspath $(CPY_INSTALL))/lib/python$(CPY_VERSION)" \
	  $(abspath $(VENV))/bin/meson setup builddir-wasi \
	    --cross-file $(abspath $(PANDAS_CROSS_INI))

$(PANDAS_PATCH_STAMP): $(PANDAS_SRC)
	@for f in \
	  $(PANDAS_SRC)/pandas/_libs/src/vendored/numpy/datetime/np_datetime.c \
	  $(PANDAS_SRC)/pandas/_libs/include/pandas/vendored/numpy/datetime/np_datetime.h \
	  $(PANDAS_SRC)/pandas/_libs/src/datetime/pd_datetime.c ; do \
	  [ -f "$$f" ] || { echo "ERROR: not found: $$f"; exit 1; }; \
	  perl -pi -e 's/(?<![.>\w])get_datetime_metadata_from_dtype\b/pd_get_datetime_metadata_from_dtype/g' "$$f"; \
	  echo "  patched $$f"; \
	done
	@touch $@

numpy-headers: $(NUMPY_INC_STAMP)

pandas-configure: $(PANDAS_BUILDDIR)/build.ninja

pandas-verify:
	@fail=0; total=0; \
	for d in $$(find $(PANDAS_BUILDDIR)/pandas -type d -name '*.so.p'); do \
	  total=$$((total+1)); \
	  n=$$(find $$d -name '*.o' | wc -l); \
	  if [ $$n -eq 0 ]; then echo "FAILED: $$d"; fail=$$((fail+1)); fi; \
	done; \
	echo "ext modules: $$((total-fail))/$$total compiled"; \
	[ $$fail -eq 0 ] || exit 1

pandas-symbols:
	@$(WASI_SDK_DIR)/bin/llvm-nm --defined-only \
	  $$(find $(NUMPY_BUILDDIR) -name '*.o' -path '*.so.p/*' \
	     ! -path '*tests*' ! -path '*_simd*' \
	     ! -path '*lapack_lite_f2c*' ! -path '*lapack_lite_python_xerbla*') \
	  $$(find $(PANDAS_BUILDDIR) -name '*.o' -path '*.so.p/*' ! -path '*tests*') \
	  2>/dev/null | awk '$$2 ~ /^[TDBR]$$/ {print $$3}' | sort | uniq -d

numpy-objs: $(NUMPY_BUILDDIR)/build.ninja
	@echo "numpy objects: $$(find $(NUMPY_UMATH_DIR) -name '*.o' | wc -l)"

numpy-configure: $(NUMPY_BUILDDIR)/build.ninja

numpy-compile: $(NUMPY_BUILDDIR)/build.ninja
	-cd $(NUMPY_SRC) && \
	  PATH="$(abspath $(VENV))/bin:$$PATH" \
	  $(VENV)/bin/ninja -C builddir-wasi -k 0
	@echo "numpy objects: $$(find $(NUMPY_BUILDDIR) -name '*.o' | wc -l)"

all: $(OUT) $(STDLIB_ZIP)

toolchains: $(CC) $(HOST_PYTHON)
	@echo "toolchains ready: wasi-sdk $(WASI_SDK_VERSION) + python $(PBS_PYVER) + venv"

install: $(CC)

$(CC):
	@echo "installing $(WASI_SDK_NAME) ..."
	mkdir -p toolchain
	curl -fL $(WASI_SDK_URL) | tar xz -C toolchain
	@test -x $(CC) || { echo "ERROR: $(CC) not found after extraction"; exit 1; }

$(HOST_PY):
	@echo "installing standalone python $(PBS_PYVER) (tag $(PBS_TAG)) ..."
	mkdir -p toolchain
	curl -fL $(PBS_URL) | tar xz -C toolchain
	@test -x $(HOST_PY) || { \
	  echo "ERROR: expected $(HOST_PY) after extraction."; \
	  echo "check the archive layout: find toolchain/python -name 'python3*'"; \
	  exit 1; }
	@if [ -d toolchain/python/include/python$(CPY_VERSION) ]; then \
	  mv toolchain/python/include/python$(CPY_VERSION) \
	     toolchain/python/include/python$(CPY_VERSION).hidden; \
	fi

$(HOST_PYTHON): $(HOST_PY)
	$(HOST_PY) -m venv $(VENV)
	$(VENV)/bin/pip install --upgrade pip
	$(VENV)/bin/pip install meson ninja meson-python cython

$(CPY_SRC_DIR):
	mkdir -p build
	git clone --depth 1 --branch $(CPY_REF) $(CPY_REPO) $(CPY_SRC_DIR)

$(CPY_INSTALL): $(CC) $(CPY_SRC_DIR) $(HOST_PYTHON) $(ZLIB_INSTALL)/lib/libz.a
	@echo "building CPython for wasm32-wasip1 via Tools/wasm/wasi.py ..."
	mkdir -p $(CPY_SRC_DIR)/cross-build/wasm32-wasip1/Modules
	@echo "*static*" > $(CPY_SRC_DIR)/cross-build/wasm32-wasip1/Modules/Setup.local
	@echo "zlib zlibmodule.c -I$(abspath $(ZLIB_INSTALL))/include -L$(abspath $(ZLIB_INSTALL))/lib -lz" >> $(CPY_SRC_DIR)/cross-build/wasm32-wasip1/Modules/Setup.local
	cd $(CPY_SRC_DIR) && \
	  WASI_SDK_PATH=$(WASI_SDK_DIR) \
	  $(HOST_PYTHON) Tools/wasm/wasi.py build
	$(MAKE) -C $(CPY_SRC_DIR)/cross-build/wasm32-wasip1
	$(MAKE) -C $(CPY_SRC_DIR)/cross-build/wasm32-wasip1 install \
	  prefix=$(abspath $(CPY_INSTALL))
	@test -f $(CPY_INSTALL)/lib/libpython$(CPY_VERSION).a || \
	  { echo "ERROR: libpython$(CPY_VERSION).a not found under $(CPY_INSTALL)/lib"; exit 1; }

sango_run.h: sango_run.py
	xxd -i sango_run.py > sango_run.h

EXT_OBJS := $(MS_OBJ)

NUMPY_STAMP := $(NUMPY_BUILDDIR)/numpy/_core/libnpymath.a

$(NUMPY_STAMP): $(NUMPY_BUILDDIR)/build.ninja
	-cd $(NUMPY_SRC) && \
	  PATH="$(abspath $(VENV))/bin:$$PATH" \
	  $(VENV)/bin/ninja -C builddir-wasi -k 0
	@test -f $(NUMPY_STAMP) || { echo "ERROR: numpy build did not produce $(NUMPY_STAMP)"; exit 1; }

PANDAS_STAMP := build/.stamp-pandas-compile

$(PANDAS_STAMP): $(PANDAS_BUILDDIR)/build.ninja
	cd $(PANDAS_SRC) && \
	  PATH="$(abspath $(VENV))/bin:$$PATH" \
	  $(VENV)/bin/ninja -C builddir-wasi
	@n=$$(find $(PANDAS_BUILDDIR) -name '*.o' -path '*.so.p/*' | wc -l | tr -d ' '); \
	 echo "pandas objects: $$n"; \
	 [ "$$n" -gt 0 ] || { echo "ERROR: no pandas objects produced"; exit 1; }
	@touch $@

pandas-compile: $(PANDAS_STAMP)

$(OUT): $(CC) $(CPY_INSTALL) sango_cpython.c sango_run.h sango_cpython_pandas.h $(EXT_OBJS) $(NUMPY_STAMP) $(PANDAS_STAMP) $(ZLIB_INSTALL)/lib/libz.a
	$(CXX) $(CFLAGS) -x c sango_cpython.c -x none \
	  $$(find $(CPY_HACL) -name '*.o' 2>/dev/null) \
	  $(EXT_OBJS) \
		$$(find $(NUMPY_BUILDDIR) -name '*.o' -path '*.so.p/*' \
	     ! -path '*tests*' ! -path '*_simd*' \
	     ! -path '*_umath_linalg*lapack_lite_f2c*' \
	     ! -path '*_umath_linalg*lapack_lite_python_xerbla*' 2>/dev/null) \
		$(NUMPY_BUILDDIR)/numpy/_core/libnpymath.a \
		$$(find $(PANDAS_BUILDDIR) -name '*.o' -path '*.so.p/*' \
	     ! -path '*tests*' 2>/dev/null) \
	  $(NUMPY_BUILDDIR)/numpy/_core/lib_multiarray_umath_mtargets.a \
	  $(NUMPY_BUILDDIR)/numpy/_core/libhighway.a \
	  $(NUMPY_BUILDDIR)/numpy/random/libnpyrandom.a \
	  -o $(OUT).raw $(LDFLAGS)
	wasm-opt --translate-to-exnref $(OUT).raw -o $(OUT)
	@rm -f $(OUT).raw
	@ls -lh $(OUT)

stdlib: $(STDLIB_ZIP)

$(STDLIB_ZIP): $(CPY_INSTALL) $(MS_SRC) $(NUMPY_SRC) $(PANDAS_SRC) $(PANDAS_STAMP) $(PY_DEPS_DIR) $(SHIM_SRCS) Makefile
	@rm -f $(STDLIB_ZIP)
	@echo "packaging stdlib into $(STDLIB_ZIP) ..."
	cd $(CPY_INSTALL)/lib/python$(CPY_VERSION) && \
		zip -qr $(abspath $(STDLIB_ZIP)) . \
			-x "test/*" "*/test/*" "*/tests/*" \
			"*/__pycache__/*" "*.pyc" \
			"ctypes/*" \
			"idlelib/*" "tkinter/*" "turtledemo/*" \
      "ensurepip/*" "pydoc_data/*"
	@echo "adding markupsafe python sources ..."
	cd $(MS_SRC)/src && \
		zip -qr $(abspath $(STDLIB_ZIP)) markupsafe \
		-x "*/__pycache__/*" "*.pyc" "*.c" "*.pyi"
	@echo "adding numpy python sources ..."
	cd $(NUMPY_SRC) && \
		zip -qr $(abspath $(STDLIB_ZIP)) numpy \
		-x "*/__pycache__/*" "*.pyc" "*.c" "*.h" "*.cpp" "*.pyx" "*.pxd" "*.src" \
		   "*/tests/*" "*/test/*" "numpy/_core/src/*" "numpy/random/src/*" \
		   "numpy/f2py/src/*" "*.so"
	@echo "adding numpy generated files ..."
	cd $(NUMPY_BUILDDIR) && \
		zip -qr $(abspath $(STDLIB_ZIP)) numpy/version.py numpy/__config__.py 2>/dev/null || true
	@echo "adding pandas python sources ..."
	cd $(PANDAS_SRC) && \
		zip -qr $(abspath $(STDLIB_ZIP)) pandas \
		-x "*/__pycache__/*" "*.pyc" "*.c" "*.h" "*.cpp" "*.pyx" "*.pxd" "*.pxi" \
		   "*/tests/*" "pandas/_libs/src/*" "pandas/_libs/include/*" \
		   "pandas/conftest.py" "*.so"
	@echo "adding pandas generated files ..."
	@src=$$(find $(PANDAS_BUILDDIR) -name '_version_meson.py' | head -1); \
	 [ -n "$$src" ] || { echo "ERROR: _version_meson.py not found"; exit 1; }; \
	 mkdir -p build/pandas-gen/pandas && cp "$$src" build/pandas-gen/pandas/
	cd build/pandas-gen && \
		zip -qr $(abspath $(STDLIB_ZIP)) pandas/_version_meson.py
	@echo "adding pure-python deps ..."
	cd $(PY_DEPS_DIR) && \
		zip -qr $(abspath $(STDLIB_ZIP)) . \
		-x "*/__pycache__/*" "*.pyc" "*.dist-info/*" "bin/*"
	@echo "adding wasip1 shims ..."
	cd $(SHIM_DIR) && \
		zip -qr $(abspath $(STDLIB_ZIP)) . -x "*/__pycache__/*" "*.pyc"

clean:
	rm -rf build $(OUT) $(STDLIB_ZIP)

distclean: clean
	rm -rf toolchain