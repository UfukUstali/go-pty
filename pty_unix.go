//go:build linux || darwin
// +build linux darwin

package lib

import (
	"io"
	"os/exec"
)

type unixPty struct {
	// fields specific to Unix implementation
}

func (p *unixPty) Resize(size PtySize) error {
	// Unix-specific implementation
	return nil
}

func (p *unixPty) GetSize() (PtySize, error) {
	// Unix-specific implementation
	return PtySize{}, nil
}

func (p *unixPty) TakeReader() (io.Reader, error) {
	// Unix-specific implementation
	return nil, nil
}

func (p *unixPty) TakeWriter() (io.Writer, error) {
	// Unix-specific implementation
	return nil, nil
}

func (p *unixPty) SpawnCommand(cmd *exec.Cmd) (Child, error) {
	// Unix-specific implementation
	return nil, nil
}

func (p *unixPty) Close() error {
	// Unix-specific implementation
	return nil
}

func NewPty() Pty {
	return &unixPty{}
}
