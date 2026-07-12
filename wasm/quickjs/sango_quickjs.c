#include <stdint.h>
#include <stdlib.h>
#include <string.h>
 
#include "quickjs.h"

#define TAG_OK    0x00
#define TAG_ERROR 0x01

static JSRuntime *g_rt;
static JSContext *g_ctx;

__attribute__((export_name("allocate")))
void *allocate(size_t size) {
    return malloc(size);
}

__attribute__((export_name("deallocate")))
void deallocate(void *ptr, size_t size) {
    (void)size;
    free(ptr);
}

__attribute__((export_name("initialize")))
uint32_t initialize(void) {
    g_rt = JS_NewRuntime();
    if (!g_rt) return 1;

    JS_SetMaxStackSize(g_rt, 0);
 
    g_ctx = JS_NewContext(g_rt);
    if (!g_ctx) return 2;
    
    return 0;
}

static uint64_t pack(uint8_t tag, const char *payload, size_t len) {
    uint8_t *buf = malloc(len + 1);
    if (!buf) return 0;
    buf[0] = tag;
    memcpy(buf + 1, payload, len);
    return ((uint64_t)(uint32_t)(uintptr_t)buf << 32) | (uint32_t)(len + 1);
}

static uint64_t pack_exception(void) {
    JSValue exc = JS_GetException(g_ctx);
    const char *msg = JS_ToCString(g_ctx, exc);
    uint64_t out;
    if (msg) {
        out = pack(TAG_ERROR, msg, strlen(msg));
        JS_FreeCString(g_ctx, msg);
    } else {
        out = pack(TAG_ERROR, "unknown error", 13);
    }
    JS_FreeValue(g_ctx, exc);
    return out;
}

__attribute__((export_name("eval")))
uint64_t eval(const char *code_ptr, size_t code_len) {
    /* QuickJS は '\0' 終端の入力を要求するため、+1 バイトでコピーして終端。
     * ホストが書き込んだバッファ(code_ptr)を直接いじらないための防御でもある */
    char *code = malloc(code_len + 1);
    if (!code) return pack(TAG_ERROR, "oom", 3);
    memcpy(code, code_ptr, code_len);
    code[code_len] = '\0';
 
    JSValue v = JS_Eval(g_ctx, code, code_len, "<sango>", JS_EVAL_TYPE_GLOBAL);
    free(code);
 
    if (JS_IsException(v)) {
        JS_FreeValue(g_ctx, v);
        return pack_exception();
    }
 
    const char *s = JS_ToCString(g_ctx, v);
    if (!s) {
        JS_FreeValue(g_ctx, v);
        return pack_exception();
    }
    uint64_t out = pack(TAG_OK, s, strlen(s));

    JS_FreeCString(g_ctx, s);
    JS_FreeValue(g_ctx, v);
    return out;
}