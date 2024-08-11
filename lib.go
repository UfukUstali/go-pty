//go:build linux || darwin || windows
// +build linux darwin windows

package lib

import (
	"errors"
	"io"
	"os/exec"
)

type PtySize struct {
	Rows        uint16
	Cols        uint16
	PixelWidth  uint16
	PixelHeight uint16
}

func DefaultPtySize() PtySize {
	return PtySize{
		Rows:        24,
		Cols:        80,
		PixelWidth:  0,
		PixelHeight: 0,
	}
}

type Pty interface {
	// Resize the window size for the pty
	Resize(size PtySize) error

	// Get the size of the pty
	GetSize() (PtySize, error)

	// Get a readable handle (recommended to be used with 8192 byte buffer)
	TakeReader() (io.Reader, error)

	// Get a writable handle
	TakeWriter() (io.Writer, error)

	// Spawn a command in the pty
	SpawnCommand(cmd *exec.Cmd) (Child, error)

	// Close the pty
	// This should be called after the child process has exited
	//
	Close() error
}

type Child interface {
	// Non-blocking check if the child has completed.
	// The first return value is the exit code but is only valid if there is no error
	// If the error is `NotFinishedError` the process has not yet exited
	Exited() (uint32, error)

	// Block until the child process completes
	// The first return value is the exit code but is only valid if there is no error
	Wait() (uint32, error)

	// Terminate the child process
	Kill() error
}

var ErrNotFinished = errors.New("not finished")

var ErrAlreadyTaken = errors.New("already taken")

var ErrAlreadyClosed = errors.New("already closed")
