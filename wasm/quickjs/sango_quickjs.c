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

/* ホストが貸した関数を呼ぶ import。ゲストにネットワークやファイルはなく、
 * 外の世界に届く経路はホストが WithHostFunc で登録した関数だけになる。
 * 戻り値は (ptr << 32 | len)。buf[0] がタグ、以降がペイロード。
 * buf はホストがゲストの allocate で確保するので、ゲストが free する。 */
__attribute__((import_module("sango"), import_name("call")))
extern uint64_t sango_host_call(const char *name, uint32_t name_len,
                                const char *arg, uint32_t arg_len);

/* JS: sango.call(name: string, arg?: string): string */
static JSValue js_sango_call(JSContext *ctx, JSValueConst this_val,
                             int argc, JSValueConst *argv) {
    (void)this_val;
    if (argc < 1) return JS_ThrowTypeError(ctx, "sango.call: name is required");

    size_t name_len, arg_len = 0;
    const char *name = JS_ToCStringLen(ctx, &name_len, argv[0]);
    if (!name) return JS_EXCEPTION;
    const char *arg = "";
    if (argc >= 2 && !JS_IsUndefined(argv[1])) {
        arg = JS_ToCStringLen(ctx, &arg_len, argv[1]);
        if (!arg) { JS_FreeCString(ctx, name); return JS_EXCEPTION; }
    }

    uint64_t r = sango_host_call(name, (uint32_t)name_len, arg, (uint32_t)arg_len);
    JS_FreeCString(ctx, name);
    if (argc >= 2 && !JS_IsUndefined(argv[1])) JS_FreeCString(ctx, arg);

    uint8_t *buf = (uint8_t *)(uintptr_t)(uint32_t)(r >> 32);
    uint32_t len = (uint32_t)r;
    if (!buf || len == 0) return JS_ThrowInternalError(ctx, "sango.call: host call failed");

    JSValue ret;
    if (buf[0] == TAG_OK) {
        ret = JS_NewStringLen(ctx, (const char *)buf + 1, len - 1);
    } else {
        ret = JS_ThrowInternalError(ctx, "%.*s", (int)(len - 1), (const char *)buf + 1);
    }
    free(buf);
    return ret;
}

static int install_host_bindings(JSContext *ctx) {
    JSValue global = JS_GetGlobalObject(ctx);
    JSValue obj = JS_NewObject(ctx);
    JS_SetPropertyStr(ctx, obj, "call", JS_NewCFunction(ctx, js_sango_call, "call", 2));
    int rc = JS_SetPropertyStr(ctx, global, "sango", obj);
    JS_FreeValue(ctx, global);
    return rc < 0 ? -1 : 0;
}

__attribute__((export_name("initialize")))
uint32_t initialize(void) {
    g_rt = JS_NewRuntime();
    if (!g_rt) return 1;

    JS_SetMaxStackSize(g_rt, 0);
 
    g_ctx = JS_NewContext(g_rt);
    if (!g_ctx) return 2;

    if (install_host_bindings(g_ctx) != 0) return 3;
    
    return 0;
}

static const char *result_to_cstring(JSValue v) {
    if (JS_IsObject(v) && !JS_IsFunction(g_ctx, v)) {
        JSValue json = JS_JSONStringify(g_ctx, v, JS_UNDEFINED, JS_UNDEFINED);
        if (JS_IsException(json)) {
            JS_FreeValue(g_ctx, JS_GetException(g_ctx));
        } else if (!JS_IsUndefined(json)) {
            const char *s = JS_ToCString(g_ctx, json);
            JS_FreeValue(g_ctx, json);
            if (s) return s;
        } else {
            JS_FreeValue(g_ctx, json);
        }
    }
    return JS_ToCString(g_ctx, v);
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
 
    const char *s = result_to_cstring(v);
    if (!s) {
        JS_FreeValue(g_ctx, v);
        return pack_exception();
    }
    uint64_t out = pack(TAG_OK, s, strlen(s));

    JS_FreeCString(g_ctx, s);
    JS_FreeValue(g_ctx, v);
    return out;
}