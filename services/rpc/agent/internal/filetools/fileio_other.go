//go:build !linux

package filetools

import (
	"errors"
	"os"
)

const secureFileIOSupported = false

func openRegularFile(*os.Root, string) (*os.File, error) {
	return nil, errors.New("file tools require the supported Linux filesystem backend")
}
