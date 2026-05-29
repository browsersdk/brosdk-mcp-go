//go:build !windows

package brosdk

func loadNative(_ string, _ func(Event)) (nativeLib, error) {
	return nil, sdkError("dynamic BroSDK loading is only supported on Windows")
}
