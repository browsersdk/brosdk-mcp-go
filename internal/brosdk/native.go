package brosdk

// nativeLib abstracts platform-specific DLL/dylib calls.
type nativeLib interface {
	registerResultCB(eventSink func(Event)) error
	init(jsonBody string) (int32, string, error)
	initAsync(jsonBody string) (int32, error)
	info() (int32, string, error)
	shutdown() (int32, error)
	tokenUpdate(jsonBody string) (int32, error)
	browserInstall(jsonBody string) (int32, error)
	browserInfo() (int32, string, error)
	browserOpen(jsonBody string) (int32, error)
	browserClose(jsonBody string) (int32, error)
	envCreate(jsonBody string) (int32, string, error)
	envPage(jsonBody string) (int32, string, error)
	envUpdate(jsonBody string) (int32, string, error)
	envDestroy(jsonBody string) (int32, string, error)
}
