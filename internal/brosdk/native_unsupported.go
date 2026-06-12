//go:build !windows && !darwin

package brosdk

import "runtime"

func loadNative(_ string, _ func(Event), _ func(CookiesEvent)) (nativeLib, error) {
	return nil, sdkError("dynamic BroSDK loading is not supported on " + runtime.GOOS)
}
