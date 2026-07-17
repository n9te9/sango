#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <wchar.h>

#define PY_SSIZE_T_CLEAN
#include <Python.h>
#include "sango_run.h"

#define TAG_OK    0x00
#define TAG_ERROR 0x01

static PyObject *g_globals = NULL;

__attribute__((export_name("allocate")))
void *allocate(size_t size) {
    return malloc(size);
}

__attribute__((export_name("deallocate")))
void deallocate(void *ptr, size_t size) {
    (void)size;
    free(ptr);
}

static uint64_t pack(uint8_t tag, const char *payload, size_t len) {
    uint8_t *buf = (uint8_t *)malloc(len + 1);
    if (!buf) return 0;
    buf[0] = tag;
    if (len) memcpy(buf + 1, payload, len);
    return ((uint64_t)(uint32_t)(uintptr_t)buf << 32) | (uint32_t)(len + 1);
}

static uint64_t pack_cstr(uint8_t tag, char *s) {
    return pack(tag, s, strlen(s));
}

static uint64_t pack_pyerror(void) {
    if (!PyErr_Occurred()) {
        return pack_cstr(TAG_ERROR, "unknown error");
    }

    PyObject *type = NULL, *val = NULL, *tb = NULL;
    PyErr_Fetch(&type, &val, &tb);
    PyErr_NormalizeException(&type, &val, &tb);

    PyObject *s = val ? PyObject_Str(val) : NULL;
    uint64_t out;
    if (s) {
        Py_ssize_t n = 0;
        const char *u = PyUnicode_AsUTF8AndSize(s, &n);
        out = u ? pack(TAG_ERROR, u, (size_t)n) : pack_cstr(TAG_ERROR, "error (utf8 decode failed)");
        Py_DECREF(s);
    } else {
        out = pack_cstr(TAG_ERROR, "unknown error");
    }
    Py_XDECREF(type);
    Py_XDECREF(val);
    Py_XDECREF(tb);
    PyErr_Clear();
    return out;
}

__attribute__((export_name("initialize")))
uint32_t initialize(void) {
    PyConfig cfg;
    PyConfig_InitIsolatedConfig(&cfg);

    cfg.parse_argv = 0;
    cfg.install_signal_handlers = 0;

    PyStatus st;

    st = PyConfig_SetString(&cfg, &cfg.home, L"/");
    if (PyStatus_Exception(st)) goto fail;

    cfg.module_search_paths_set = 1;
    st = PyWideStringList_Append(&cfg.module_search_paths, L"/lib/python3.13");
    if (PyStatus_Exception(st)) goto fail;

    st = PyWideStringList_Append(&cfg.module_search_paths, L"/lib/python3.13/lib-dynload");
    if (PyStatus_Exception(st)) goto fail;

    st = Py_InitializeFromConfig(&cfg);
    if (PyStatus_Exception(st)) goto fail;
    PyConfig_Clear(&cfg);

    PyObject *main_mod = PyImport_AddModule("__main__");
    if (!main_mod) return 2;

    g_globals = PyModule_GetDict(main_mod);
    Py_INCREF(g_globals);

    char *helper = malloc(sango_run_py_len + 1);
    memcpy(helper, sango_run_py, sango_run_py_len);
    helper[sango_run_py_len] = '\0';
    if (PyRun_SimpleString(helper) != 0) { free(helper); return 3; }
    free(helper);

    return 0;

fail:
    PyConfig_Clear(&cfg);
    return 1;
}

__attribute__((export_name("eval")))
uint64_t eval(const char *code_ptr, size_t code_len) {
    if (!g_globals) {
        return pack_cstr(TAG_ERROR, "not initialized");
    }

    char *code = (char *)malloc(code_len + 1);
    if (!code) return pack_cstr(TAG_ERROR, "oom");
    if (code_len) memcpy(code, code_ptr, code_len);
    code[code_len] = '\0';

    PyObject *runner = PyDict_GetItemString(g_globals, "__sango_run");
    if (!runner) {
        return pack_cstr(TAG_ERROR, "runner __sango_run not loaded");
    }
    PyObject *v = PyObject_CallFunction(runner, "s#O", code_ptr, (Py_ssize_t)code_len, g_globals);
    if (!v) {
        return pack_pyerror();
    }

    PyObject *r = PyObject_Repr(v);
    Py_DECREF(v);
    if (!r) return pack_pyerror();

    Py_ssize_t n = 0;
    const char *u = PyUnicode_AsUTF8AndSize(r, &n);
    uint64_t out = u ? pack(TAG_OK, u, (size_t)n) : pack_pyerror();
    Py_DECREF(r);
    return out;
}