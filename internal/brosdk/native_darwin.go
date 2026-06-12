//go:build darwin

package brosdk

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>
#include <stdint.h>

// ── function pointer typedefs matching brosdk.h ──
typedef int (*sdk_register_result_cb_fn)(uintptr_t cb, uintptr_t user_data);
typedef void (*sdk_cookies_storage_cb_fn)(const char *data, size_t len,
                                          char **new_data, size_t *new_len,
                                          void *user_data);
typedef int (*sdk_register_cookies_storage_cb_fn)(uintptr_t cb, uintptr_t user_data);

typedef int (*sdk_init_fn)(
	uintptr_t handle,
	const char *data,
	int len,
	char **out,
	size_t *out_len,
);
typedef int (*sdk_init_async_fn)(uintptr_t handle, const char *data, int len);
typedef int (*sdk_info_fn)(char **out, size_t *out_len);
typedef int (*sdk_shutdown_fn)(void);
typedef int (*sdk_token_update_fn)(const char *data, int len);
typedef int (*sdk_browser_install_fn)(const char *data, int len);
typedef int (*sdk_browser_info_fn)(char **out, size_t *out_len);
typedef int (*sdk_browser_open_fn)(const char *data, int len);
typedef int (*sdk_browser_close_fn)(const char *data, int len);
typedef int (*sdk_env_create_fn)(const char *data, int len, char **out, size_t *out_len);
typedef int (*sdk_env_page_fn)(const char *data, int len, char **out, size_t *out_len);
typedef int (*sdk_env_update_fn)(const char *data, int len, char **out, size_t *out_len);
typedef int (*sdk_env_destroy_fn)(const char *data, int len, char **out, size_t *out_len);
typedef void*(*sdk_malloc_fn)(size_t size);
typedef void (*sdk_free_fn)(void *p);

// ── lookup helper ──
static void* lookup_sym(void* handle, const char* name) {
	return dlsym(handle, name);
}

// ── typed call helpers ──

static inline int call_init(void* fn, uintptr_t handle, const char* d, int len, char** out, size_t* out_len) {
	return ((sdk_init_fn)fn)(handle, d, len, out, out_len);
}
static inline int call_init_async(void* fn, uintptr_t handle, const char* d, int len) {
	return ((sdk_init_async_fn)fn)(handle, d, len);
}
static inline int call_sync_no_args(void* fn, char** out, size_t* out_len) {
	return ((sdk_info_fn)fn)(out, out_len);
}
static inline int call_shutdown(void* fn) {
	return ((sdk_shutdown_fn)fn)();
}
static inline int call_async_json(void* fn, const char* d, int len) {
	// tokenUpdate, browserInstall, browserOpen, browserClose all share this signature
	return ((sdk_token_update_fn)fn)(d, len);
}
static inline int call_sync_json(void* fn, const char* d, int len, char** out, size_t* out_len) {
	return ((sdk_env_create_fn)fn)(d, len, out, out_len);
}

// ── callback bridge ──
//
// goSdkResultCallback and goSdkCookiesCallback are exported Go functions.
// We expose typed pass-throughs so the SDK can call into Go.

extern void goSdkResultCallback(int code, void* userData, const char* data, int len);
extern void goSdkCookiesCallback(const char* data, size_t len, char** newData, size_t* newLen, void* userData);

static sdk_register_result_cb_fn get_register_cb(void* handle) {
	return (sdk_register_result_cb_fn)dlsym(handle, "sdk_register_result_cb");
}

static int call_register_cookies_cb(void* fn, uintptr_t cb, uintptr_t user_data) {
	return ((sdk_register_cookies_storage_cb_fn)fn)(cb, user_data);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// ── package-level singletons for callback bridging ──

var activeEventSink func(Event)
var activeCookiesSink func(CookiesEvent)

// darwinLib wraps a dlopen'd dylib handle and pre-resolved function pointers.
type darwinLib struct {
	handle unsafe.Pointer // void* from dlopen

	pInit          unsafe.Pointer
	pInitAsync     unsafe.Pointer
	pInfo          unsafe.Pointer
	pShutdown      unsafe.Pointer
	pTokenUpdate   unsafe.Pointer
	pBrowserInstall unsafe.Pointer
	pBrowserInfo   unsafe.Pointer
	pBrowserOpen   unsafe.Pointer
	pBrowserClose  unsafe.Pointer
	pEnvCreate     unsafe.Pointer
	pEnvPage       unsafe.Pointer
	pEnvUpdate     unsafe.Pointer
	pEnvDestroy    unsafe.Pointer
	pMalloc        unsafe.Pointer
	pFree          unsafe.Pointer
	pRegCB         unsafe.Pointer
	pRegCookiesCB  unsafe.Pointer
}

// loadNative opens the dylib at path and registers the result and cookie callbacks.
func loadNative(path string, eventSink func(Event), cookieSink func(CookiesEvent)) (nativeLib, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	handle := C.dlopen(cPath, C.RTLD_LAZY)
	if handle == nil {
		return nil, fmt.Errorf("dlopen: %s", C.GoString(C.dlerror()))
	}

	lib := &darwinLib{handle: handle}
	if err := lib.resolve(); err != nil {
		C.dlclose(handle)
		return nil, err
	}

	activeEventSink = eventSink
	activeCookiesSink = cookieSink

	// Register result callback — pass the exported Go callback as a C function pointer.
	regCB := *(**C.sdk_register_result_cb_fn)(unsafe.Pointer(&lib.pRegCB))
	code := regCB(C.uintptr_t(uintptr(C.goSdkResultCallback)), 0)
	if int32(code) < 0 {
		return nil, sdkError(fmt.Sprintf("sdk_register_result_cb failed: code=%d", int32(code)))
	}

	// Register cookies storage callback.
	code2 := C.call_register_cookies_cb(lib.pRegCookiesCB,
		C.uintptr_t(uintptr(C.goSdkCookiesCallback)), 0)
	if int32(code2) < 0 {
		return nil, sdkError(fmt.Sprintf("sdk_register_cookies_storage_cb failed: code=%d", int32(code2)))
	}

	return lib, nil
}

func (l *darwinLib) resolve() error {
	syms := map[string]*unsafe.Pointer{
		"sdk_init":                 &l.pInit,
		"sdk_init_async":           &l.pInitAsync,
		"sdk_info":                 &l.pInfo,
		"sdk_shutdown":             &l.pShutdown,
		"sdk_token_update":         &l.pTokenUpdate,
		"sdk_browser_install":      &l.pBrowserInstall,
		"sdk_browser_info":         &l.pBrowserInfo,
		"sdk_browser_open":         &l.pBrowserOpen,
		"sdk_browser_close":        &l.pBrowserClose,
		"sdk_env_create":           &l.pEnvCreate,
		"sdk_env_page":             &l.pEnvPage,
		"sdk_env_update":           &l.pEnvUpdate,
		"sdk_env_destroy":          &l.pEnvDestroy,
		"sdk_malloc":               &l.pMalloc,
		"sdk_free":                 &l.pFree,
		"sdk_register_result_cb":          &l.pRegCB,
		"sdk_register_cookies_storage_cb": &l.pRegCookiesCB,
	}

	for name, ptr := range syms {
		cName := C.CString(name)
		p := C.lookup_sym(l.handle, cName)
		C.free(unsafe.Pointer(cName))
		if p == nil {
			return fmt.Errorf("dlsym %s: %s", name, C.GoString(C.dlerror()))
		}
		*ptr = p
	}
	return nil
}

//export goSdkResultCallback
func goSdkResultCallback(code C.int, _ unsafe.Pointer, data *C.char, length C.int) {
	if activeEventSink == nil {
		return
	}
	goData := C.GoStringN(data, length)
	activeEventSink(Event{
		Code: int32(code),
		Data: goData,
	})
}

//export goSdkCookiesCallback
func goSdkCookiesCallback(data *C.char, length C.size_t, _ **C.char, _ *C.size_t, _ unsafe.Pointer) {
	if activeCookiesSink == nil {
		return
	}
	goData := C.GoStringN(data, C.int(length))
	activeCookiesSink(CookiesEvent{
		Data: goData,
	})
}

// ---------- nativeLib interface implementation ----------

func (l *darwinLib) registerResultCB(_ func(Event)) error {
	return nil // already registered in loadNative
}

func (l *darwinLib) registerCookiesStorageCB(_ func(CookiesEvent)) error {
	return nil // already registered in loadNative
}

func (l *darwinLib) init(jsonBody string) (int32, string, error) {
	cBody := C.CString(jsonBody)
	defer C.free(unsafe.Pointer(cBody))

	var out *C.char
	var outLen C.size_t

	code := C.call_init(l.pInit, 0, cBody, C.int(len(jsonBody)), &out, &outLen)
	return int32(code), l.takeString(out, outLen), nil
}

func (l *darwinLib) initAsync(jsonBody string) (int32, error) {
	cBody := C.CString(jsonBody)
	defer C.free(unsafe.Pointer(cBody))
	return int32(C.call_init_async(l.pInitAsync, 0, cBody, C.int(len(jsonBody)))), nil
}

func (l *darwinLib) info() (int32, string, error) {
	var out *C.char
	var outLen C.size_t
	code := C.call_sync_no_args(l.pInfo, &out, &outLen)
	return int32(code), l.takeString(out, outLen), nil
}

func (l *darwinLib) shutdown() (int32, error) {
	return int32(C.call_shutdown(l.pShutdown)), nil
}

func (l *darwinLib) tokenUpdate(jsonBody string) (int32, error) {
	cBody := C.CString(jsonBody)
	defer C.free(unsafe.Pointer(cBody))
	return int32(C.call_async_json(l.pTokenUpdate, cBody, C.int(len(jsonBody)))), nil
}

func (l *darwinLib) browserInstall(jsonBody string) (int32, error) {
	cBody := C.CString(jsonBody)
	defer C.free(unsafe.Pointer(cBody))
	return int32(C.call_async_json(l.pBrowserInstall, cBody, C.int(len(jsonBody)))), nil
}

func (l *darwinLib) browserInfo() (int32, string, error) {
	var out *C.char
	var outLen C.size_t
	code := C.call_sync_no_args(l.pBrowserInfo, &out, &outLen)
	return int32(code), l.takeString(out, outLen), nil
}

func (l *darwinLib) browserOpen(jsonBody string) (int32, error) {
	cBody := C.CString(jsonBody)
	defer C.free(unsafe.Pointer(cBody))
	return int32(C.call_async_json(l.pBrowserOpen, cBody, C.int(len(jsonBody)))), nil
}

func (l *darwinLib) browserClose(jsonBody string) (int32, error) {
	cBody := C.CString(jsonBody)
	defer C.free(unsafe.Pointer(cBody))
	return int32(C.call_async_json(l.pBrowserClose, cBody, C.int(len(jsonBody)))), nil
}

func (l *darwinLib) envCreate(jsonBody string) (int32, string, error) {
	cBody := C.CString(jsonBody)
	defer C.free(unsafe.Pointer(cBody))
	var out *C.char
	var outLen C.size_t
	code := C.call_sync_json(l.pEnvCreate, cBody, C.int(len(jsonBody)), &out, &outLen)
	return int32(code), l.takeString(out, outLen), nil
}

func (l *darwinLib) envPage(jsonBody string) (int32, string, error) {
	cBody := C.CString(jsonBody)
	defer C.free(unsafe.Pointer(cBody))
	var out *C.char
	var outLen C.size_t
	code := C.call_sync_json(l.pEnvPage, cBody, C.int(len(jsonBody)), &out, &outLen)
	return int32(code), l.takeString(out, outLen), nil
}

func (l *darwinLib) envUpdate(jsonBody string) (int32, string, error) {
	cBody := C.CString(jsonBody)
	defer C.free(unsafe.Pointer(cBody))
	var out *C.char
	var outLen C.size_t
	code := C.call_sync_json(l.pEnvUpdate, cBody, C.int(len(jsonBody)), &out, &outLen)
	return int32(code), l.takeString(out, outLen), nil
}

func (l *darwinLib) envDestroy(jsonBody string) (int32, string, error) {
	cBody := C.CString(jsonBody)
	defer C.free(unsafe.Pointer(cBody))
	var out *C.char
	var outLen C.size_t
	code := C.call_sync_json(l.pEnvDestroy, cBody, C.int(len(jsonBody)), &out, &outLen)
	return int32(code), l.takeString(out, outLen), nil
}

// ---------- helpers ----------

func (l *darwinLib) takeString(out *C.char, outLen C.size_t) string {
	if out == nil || outLen == 0 {
		return ""
	}
	s := C.GoStringN(out, outLen)
	// Free SDK-allocated buffer
	pFree := *(**C.sdk_free_fn)(unsafe.Pointer(&l.pFree))
	pFree(unsafe.Pointer(out))
	return s
}
