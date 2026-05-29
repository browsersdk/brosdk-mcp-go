package brosdk

import "fmt"

func sdkError(msg string) error {
	return fmt.Errorf("brosdk: %s", msg)
}
