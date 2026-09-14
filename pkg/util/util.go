package util

import (
	"os"

	logf "github.com/openshift/router/log"
)

var log = logf.Logger.WithName("util")

// CopyFile copies a file from src to dest. It returns an error on failure
func CopyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := in.Close(); err != nil {
			log.Error(err, "Failed to close input file", "filename", src)
		}
	}()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	_, err = out.ReadFrom(in)
	if err != nil {
		if closeErr := out.Close(); closeErr != nil {
			log.Error(closeErr, "Failed to close output file", "filename", dest)
		}
		return err
	}
	return out.Close()
}
