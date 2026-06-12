//go:build windows

package brosdk

import (
	"syscall"
	"unsafe"
)

// ---------- package-level singletons for callback bridging ----------

var activeEventSink func(Event)
var activeCookiesSink func(CookiesEvent)
var activeWindowsLib *windowsLib
var resultCallbackPtr uintptr
var cookiesCallbackPtr uintptr

// ---------- windowsLib ----------

type windowsLib struct {
	dll *syscall.LazyDLL

	registerResultCBProc      *syscall.LazyProc
	registerCookiesStorageCBProc *syscall.LazyProc
	initProc                  *syscall.LazyProc
	initAsyncProc             *syscall.LazyProc
	infoProc                  *syscall.LazyProc
	shutdownProc              *syscall.LazyProc
	tokenUpdateProc           *syscall.LazyProc
	browserInstallProc        *syscall.LazyProc
	browserInfoProc           *syscall.LazyProc
	browserOpenProc           *syscall.LazyProc
	browserCloseProc          *syscall.LazyProc
	envCreateProc             *syscall.LazyProc
	envPageProc               *syscall.LazyProc
	envUpdateProc             *syscall.LazyProc
	envDestroyProc            *syscall.LazyProc
	mallocProc                *syscall.LazyProc
	freeProc                  *syscall.LazyProc
}

// loadNative loads the DLL and wires up the result and cookie callbacks.
func loadNative(path string, eventSink func(Event), cookieSink func(CookiesEvent)) (nativeLib, error) {
	dll := syscall.NewLazyDLL(path)
	lib := &windowsLib{
		dll:                          dll,
		registerResultCBProc:         dll.NewProc("sdk_register_result_cb"),
		registerCookiesStorageCBProc: dll.NewProc("sdk_register_cookies_storage_cb"),
		initProc:                     dll.NewProc("sdk_init"),
		initAsyncProc:                dll.NewProc("sdk_init_async"),
		infoProc:                     dll.NewProc("sdk_info"),
		shutdownProc:                 dll.NewProc("sdk_shutdown"),
		tokenUpdateProc:              dll.NewProc("sdk_token_update"),
		browserInstallProc:           dll.NewProc("sdk_browser_install"),
		browserInfoProc:              dll.NewProc("sdk_browser_info"),
		browserOpenProc:              dll.NewProc("sdk_browser_open"),
		browserCloseProc:             dll.NewProc("sdk_browser_close"),
		envCreateProc:                dll.NewProc("sdk_env_create"),
		envPageProc:                  dll.NewProc("sdk_env_page"),
		envUpdateProc:                dll.NewProc("sdk_env_update"),
		envDestroyProc:               dll.NewProc("sdk_env_destroy"),
		mallocProc:                   dll.NewProc("sdk_malloc"),
		freeProc:                     dll.NewProc("sdk_free"),
	}

	if err := dll.Load(); err != nil {
		return nil, err
	}
	if err := lib.resolve(); err != nil {
		return nil, err
	}

	activeEventSink = eventSink
	activeCookiesSink = cookieSink
	activeWindowsLib = lib
	resultCallbackPtr = syscall.NewCallback(sdkResultCallback)

	code, _, err := lib.registerResultCBProc.Call(resultCallbackPtr, 0)
	if int32(code) < 0 {
		return nil, sdkError("sdk_register_result_cb failed: " + wrapErr(err).Error())
	}

	cookiesCallbackPtr = syscall.NewCallbackCDecl(sdkCookiesCallback)
	code2, _, err2 := lib.registerCookiesStorageCBProc.Call(cookiesCallbackPtr, 0)
	if int32(code2) < 0 {
		return nil, sdkError("sdk_register_cookies_storage_cb failed: " + wrapErr(err2).Error())
	}

	return lib, nil
}

func (l *windowsLib) resolve() error {
	procs := []*syscall.LazyProc{
		l.registerResultCBProc,
		l.registerCookiesStorageCBProc,
		l.initProc,
		l.initAsyncProc,
		l.infoProc,
		l.shutdownProc,
		l.tokenUpdateProc,
		l.browserInstallProc,
		l.browserInfoProc,
		l.browserOpenProc,
		l.browserCloseProc,
		l.envCreateProc,
		l.envPageProc,
		l.envUpdateProc,
		l.envDestroyProc,
		l.mallocProc,
		l.freeProc,
	}
	for _, p := range procs {
		if err := p.Find(); err != nil {
			return err
		}
	}
	return nil
}

// ---------- nativeLib interface implementation ----------

func (l *windowsLib) registerResultCB(_ func(Event)) error {
	// Already wired in loadNative; no-op here.
	return nil
}

func (l *windowsLib) registerCookiesStorageCB(_ func(CookiesEvent)) error {
	// Already wired in loadNative; no-op here.
	return nil
}

func (l *windowsLib) init(jsonBody string) (int32, string, error) {
	return l.callSyncJSON(l.initProc, jsonBody, true)
}

func (l *windowsLib) initAsync(jsonBody string) (int32, error) {
	return l.callAsyncJSON(l.initAsyncProc, jsonBody, true)
}

func (l *windowsLib) info() (int32, string, error) {
	return l.callSyncNoArgs(l.infoProc)
}

func (l *windowsLib) shutdown() (int32, error) {
	code, _, err := l.shutdownProc.Call()
	return int32(code), wrapErr(err)
}

func (l *windowsLib) tokenUpdate(jsonBody string) (int32, error) {
	return l.callAsyncJSON(l.tokenUpdateProc, jsonBody, false)
}

func (l *windowsLib) browserInstall(jsonBody string) (int32, error) {
	return l.callAsyncJSON(l.browserInstallProc, jsonBody, false)
}

func (l *windowsLib) browserInfo() (int32, string, error) {
	return l.callSyncNoArgs(l.browserInfoProc)
}

func (l *windowsLib) browserOpen(jsonBody string) (int32, error) {
	return l.callAsyncJSON(l.browserOpenProc, jsonBody, false)
}

func (l *windowsLib) browserClose(jsonBody string) (int32, error) {
	return l.callAsyncJSON(l.browserCloseProc, jsonBody, false)
}

func (l *windowsLib) envCreate(jsonBody string) (int32, string, error) {
	return l.callSyncJSON(l.envCreateProc, jsonBody, false)
}

func (l *windowsLib) envPage(jsonBody string) (int32, string, error) {
	return l.callSyncJSON(l.envPageProc, jsonBody, false)
}

func (l *windowsLib) envUpdate(jsonBody string) (int32, string, error) {
	return l.callSyncJSON(l.envUpdateProc, jsonBody, false)
}

func (l *windowsLib) envDestroy(jsonBody string) (int32, string, error) {
	return l.callSyncJSON(l.envDestroyProc, jsonBody, false)
}

// ---------- call helpers ----------

// callSyncNoArgs: proc(char **out_data, size_t *out_len)
func (l *windowsLib) callSyncNoArgs(proc *syscall.LazyProc) (int32, string, error) {
	var out uintptr
	var outLen uintptr
	code, _, err := proc.Call(
		uintptr(unsafe.Pointer(&out)),
		uintptr(unsafe.Pointer(&outLen)),
	)
	return int32(code), l.takeString(out, outLen), wrapErr(err)
}

// callSyncJSON: optionally passes an sdk_handle_t* as first arg (withHandle=true for sdk_init).
// proc(handle*, data, len, char **out, size_t *out_len) or proc(data, len, char **out, size_t *out_len)
func (l *windowsLib) callSyncJSON(proc *syscall.LazyProc, jsonBody string, withHandle bool) (int32, string, error) {
	body, err := syscall.BytePtrFromString(jsonBody)
	if err != nil {
		return -1, "", err
	}
	var out uintptr
	var outLen uintptr
	var code uintptr
	var callErr error

	if withHandle {
		var handle uintptr
		code, _, callErr = proc.Call(
			uintptr(unsafe.Pointer(&handle)),
			uintptr(unsafe.Pointer(body)),
			uintptr(len(jsonBody)),
			uintptr(unsafe.Pointer(&out)),
			uintptr(unsafe.Pointer(&outLen)),
		)
	} else {
		code, _, callErr = proc.Call(
			uintptr(unsafe.Pointer(body)),
			uintptr(len(jsonBody)),
			uintptr(unsafe.Pointer(&out)),
			uintptr(unsafe.Pointer(&outLen)),
		)
	}
	return int32(code), l.takeString(out, outLen), wrapErr(callErr)
}

// callAsyncJSON: withHandle=true adds an sdk_handle_t* as first arg (sdk_init_async).
func (l *windowsLib) callAsyncJSON(proc *syscall.LazyProc, jsonBody string, withHandle bool) (int32, error) {
	body, err := syscall.BytePtrFromString(jsonBody)
	if err != nil {
		return -1, err
	}
	var code uintptr
	var callErr error
	if withHandle {
		var handle uintptr
		code, _, callErr = proc.Call(
			uintptr(unsafe.Pointer(&handle)),
			uintptr(unsafe.Pointer(body)),
			uintptr(len(jsonBody)),
		)
	} else {
		code, _, callErr = proc.Call(
			uintptr(unsafe.Pointer(body)),
			uintptr(len(jsonBody)),
		)
	}
	return int32(code), wrapErr(callErr)
}

// takeString reads the SDK-allocated buffer, copies it into a Go string, then frees it.
// nolint: unsafeptr — ptr is a C heap pointer returned by sdk_malloc / sdk_info etc.
func (l *windowsLib) takeString(ptr uintptr, length uintptr) string {
	if ptr == 0 || length == 0 {
		return ""
	}
	p := unsafe.Pointer(ptr) //nolint:unsafeptr
	b := unsafe.Slice((*byte)(p), int(length))
	s := string(b)
	l.freeProc.Call(ptr)
	return s
}

func wrapErr(err error) error {
	if errno, ok := err.(syscall.Errno); ok && errno == 0 {
		return nil
	}
	return err
}

// ---------- result callback (called from DLL thread) ----------

func sdkResultCallback(code uintptr, _ uintptr, data uintptr, length uintptr) uintptr {
	if activeEventSink == nil {
		return 0
	}
	activeEventSink(Event{
		Code: int32(code),
		Data: ptrToString(data, length),
	})
	return 0
}

// sdkCookiesCallback is the __cdecl trampoline for sdk_cookies_storage_cb_t.
// Read-only: we observe the cookie data but do not modify it (new_data/new_len stay NULL/0).
func sdkCookiesCallback(data uintptr, length uintptr, _ uintptr, _ uintptr, _ uintptr) uintptr {
	if activeCookiesSink != nil {
		activeCookiesSink(CookiesEvent{
			Data: ptrToString(data, length),
		})
	}
	return 0
}

func ptrToString(data uintptr, length uintptr) string {
	if data == 0 || length == 0 {
		return ""
	}
	p := unsafe.Pointer(data) //nolint:unsafeptr
	b := unsafe.Slice((*byte)(p), int(length))
	return string(b)
}
