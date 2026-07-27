def __getattr__(name):
    raise AttributeError(
        f"ctypes.{name} is not available on wasm32-wasip1 "
        "(dynamic FFI requires runtime code generation)"
    )