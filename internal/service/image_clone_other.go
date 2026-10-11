//go:build !linux

package service

import (
	"errors"
	"os"
)

func cloneImageFile(dst, src *os.File) error {
	return errors.New("image cloning unavailable")
}
