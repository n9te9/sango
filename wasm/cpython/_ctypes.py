# _ctypes.py is a stub file for CPython's _ctypes module, which is not available in this environment.
# _ctypes is decidedly regulation and has to by-pass for wasm.
import struct

class _CData:
    def __init__(self, *args, **kwargs): pass
    @classmethod
    def from_param(cls, *args, **kwargs): return None
    @classmethod
    def in_dll(cls, *args, **kwargs): return cls()
    @classmethod
    def from_address(cls, *args, **kwargs): return cls()

class _SimpleCData(_CData):
    _type_ = "i"
class Union(_CData): pass
class Structure(_CData): pass
class Array(_CData): pass
class _Pointer(_CData): pass
class CFuncPtr(_CData): pass
class _c_object_p(_CData): pass

__version__ = "1.1.0"
RTLD_LOCAL = 0
RTLD_GLOBAL = 1
ArgumentError = Exception

FUNCFLAG_STDCALL = 0
FUNCFLAG_CDECL = 1
FUNCFLAG_USE_ERRNO = 4
FUNCFLAG_USE_LASTERROR = 8
FUNCFLAG_PYTHONAPI = 16

def _get_calcsize(typ):
    try:
        typecode = getattr(typ, "_type_", "P")
        return struct.calcsize(typecode)
    except Exception:
        return 4

def sizeof(typ, *args, **kwargs): return _get_calcsize(typ)
def alignment(typ, *args, **kwargs): return _get_calcsize(typ)
def byref(*a, **kw): return None
def addressof(*a, **kw): return 1
def pointer(*a, **kw): return None
def POINTER(*a, **kw): return _Pointer
def cast(*a, **kw): return None
def string_at(*a, **kw): return b""
def wstring_at(*a, **kw): return ""
def dlopen(*a, **kw): return 0
def dlsym(*a, **kw): return 0
def dlclose(*a, **kw): pass

class _Dummy:
    def __init__(self, *args, **kwargs): pass
    def __call__(self, *args, **kwargs): return None
    def __getattr__(self, name): return self

def __getattr__(name):
    return _Dummy