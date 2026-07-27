
ACCESS_DEFAULT = 0
ACCESS_READ = 1
ACCESS_WRITE = 2
ACCESS_COPY = 3

class error(OSError):
    pass

class mmap:
    def __init__(self, *args, **kwargs):
        raise OSError("mmap is not available on wasm32-wasip1")