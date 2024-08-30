//go:build windows
// +build windows

package lib

import (
	"io"
	"log"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var logger = log.New(os.Stdout, "go-pty", log.Lmsgprefix|log.Lshortfile)

const (
	PSEUDOCONSOLE_INHERIT_CURSOR   = 0x1
	PSEUDOCONSOLE_RESIZE_QUIRK     = 0x2
	PSEUDOCONSOLE_WIN32_INPUT_MODE = 0x4
)

type windowsReader struct {
	read windows.Handle
}

func (r *windowsReader) Read(p []byte) (int, error) {
	var n uint32
	// log.Info("Reading from pipe")
	switch err := windows.ReadFile(r.read, p, &n, nil); err {
	case windows.ERROR_BROKEN_PIPE:
		return 0, io.EOF
	case windows.ERROR_NO_DATA:
		return 0, io.EOF
	case windows.ERROR_MORE_DATA:
		return int(n), nil
	case nil:
		return int(n), nil
	default:
		logger.Println(err)
		return 0, err
	}
}

type windowsWriter struct {
	write windows.Handle
}

func (w *windowsWriter) Write(p []byte) (int, error) {
	var n uint32
	if err := windows.WriteFile(w.write, p, &n, nil); err != nil {
		logger.Println(err)
		return 0, err
	}
	return int(n), nil
}

type windowsChild struct {
	Proc windows.Handle
}

func (c *windowsChild) Exited() (uint32, error) {
	var status uint32
	if err := windows.GetExitCodeProcess(c.Proc, &status); err != nil {
		logger.Println(err)
		return 0, err
	}

	if status == 259 {
		return 0, ErrNotFinished
	}

	return status, nil
}

func (c *windowsChild) Wait() (uint32, error) {
	if c.Proc == windows.InvalidHandle {
		return 0, ErrAlreadyClosed
	}
	if _, err := windows.WaitForSingleObject(c.Proc, windows.INFINITE); err != nil {
		logger.Println(err)
		return 0, err
	}
	code, err := c.Exited()
	c.Proc = windows.InvalidHandle
	return code, err
}

func (c *windowsChild) Kill() error {
	if c.Proc == windows.InvalidHandle {
		return ErrAlreadyClosed
	}
	if err := windows.TerminateProcess(c.Proc, 1); err != nil {
		logger.Println(err)
		return err
	}
	return nil
}

type windowsPty struct {
	PCon        windows.Handle
	PtySize     PtySize
	Readable    *windowsReader
	readHandle  windows.Handle
	Writable    *windowsWriter
	writeHandle windows.Handle
	closed      bool
}

func (p *windowsPty) Resize(size PtySize) error {
	if err := windows.ResizePseudoConsole(
		p.PCon,
		windows.Coord{X: int16(size.Cols), Y: int16(size.Rows)},
	); err != nil {
		logger.Println(err)
		return err
	}

	p.PtySize = size
	return nil
}

func (p *windowsPty) GetSize() (PtySize, error) {
	return p.PtySize, nil
}

func (p *windowsPty) TakeReader() (io.Reader, error) {
	if p.Readable == nil {
		return nil, ErrAlreadyTaken
	}

	temp := p.Readable
	p.Readable = nil
	return temp, nil
}

func (p *windowsPty) TakeWriter() (io.Writer, error) {
	if p.Writable == nil {
		return nil, ErrAlreadyTaken
	}

	temp := p.Writable
	p.Writable = nil
	return temp, nil
}

func (p *windowsPty) SpawnCommand(cmd *exec.Cmd) (Child, error) {
	si := windows.StartupInfoEx{}
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Flags = windows.STARTF_USESTDHANDLES
	si.StdInput = windows.InvalidHandle
	si.StdOutput = windows.InvalidHandle
	si.StdErr = windows.InvalidHandle

	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		logger.Println(err)
		return nil, err
	}
	defer attrs.Delete()

	if err := attrs.Update(
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		unsafe.Pointer(p.PCon),
		unsafe.Sizeof(p.PCon),
	); err != nil {
		logger.Println(err)
		return nil, err
	}

	si.ProcThreadAttributeList = attrs.List()

	exe, err := syscall.UTF16PtrFromString(cmd.Path)
	if err != nil {
		logger.Println(err)
		return nil, err
	}

	cmd_str := cmd.Path
	for _, arg := range cmd.Args[1:] {
		cmd_str += " " + arg
	}

	cmd_line, err := syscall.UTF16PtrFromString(cmd_str)
	if err != nil {
		logger.Println(err)
		return nil, err
	}

	env := []uint16{}
	for _, arg := range cmd.Env {
		uint16_arg, err := syscall.UTF16FromString(arg)
		if err != nil {
			logger.Println(err)
			return nil, err
		}
		env = append(env, uint16_arg...)
	}
	env = append(env, 0)
	env_block := &env[0]

	var cwd *uint16 = nil
	if cmd.Dir != "" {
		cwd, err = syscall.UTF16PtrFromString(cmd.Dir)
		if err != nil {
			logger.Println(err)
			return nil, err
		}
	}

	pi := windows.ProcessInformation{}

	if err := windows.CreateProcess(
		exe,
		cmd_line,
		nil,
		nil,
		false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		env_block,
		cwd,
		&si.StartupInfo,
		&pi,
	); err != nil {
		logger.Println(err)
		return nil, err
	}
	err = windows.CloseHandle(pi.Thread)
	if err != nil {
		logger.Println(err)
		return nil, err
	}

	return &windowsChild{
		pi.Process,
	}, nil
}

func (p *windowsPty) Close() error {
	if p.closed {
		return ErrAlreadyClosed
	}
	go func() {
		// https://learn.microsoft.com/en-us/windows/console/closepseudoconsole#remarks
		reader := &windowsReader{p.readHandle}
		buffer := make([]byte, 4096)
		for {
			n, err := reader.Read(buffer)
			if err != nil {
				if err == io.EOF {
					break
				}
				logger.Println(err)
				return
			}
			// respond to cursor position requests otherwise the process will hang don't know why
			if n == 4 && string(buffer[:n]) == "\x1b[6n" {
				writer := &windowsWriter{p.writeHandle}
				writer.Write([]byte("\x1b[24;80R"))
			}
		}
	}()
	windows.ClosePseudoConsole(p.PCon)
	if err := windows.CloseHandle(p.readHandle); err != nil {
		logger.Println(err)
		return err
	}
	if err := windows.CloseHandle(p.writeHandle); err != nil {
		logger.Println(err)
		return err
	}
	p.closed = true
	return nil
}

type Pipe struct {
	Read  windows.Handle
	Write windows.Handle
}

func createPipe() (*Pipe, error) {
	sa := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(syscall.SecurityAttributes{})),
		SecurityDescriptor: nil,
		InheritHandle:      0,
	}
	var (
		read  windows.Handle = windows.InvalidHandle
		write windows.Handle = windows.InvalidHandle
	)

	if err := windows.CreatePipe(&read, &write, &sa, 0); err != nil {
		logger.Println(err)
		return nil, err
	}

	return &Pipe{
		Read:  read,
		Write: write,
	}, nil
}

func NewPty(size PtySize) (Pty, error) {
	stdin, err := createPipe()
	if err != nil {
		logger.Println(err)
		return nil, err
	}

	stdout, err := createPipe()
	if err != nil {
		windows.CloseHandle(stdin.Write)
		windows.CloseHandle(stdin.Read)
		logger.Println(err)
		return nil, err
	}

	PCon := windows.InvalidHandle

	coord := windows.Coord{
		X: int16(size.Cols),
		Y: int16(size.Rows),
	}

	// in.read, out.write
	if err := windows.CreatePseudoConsole(
		coord,
		stdin.Read,
		stdout.Write,
		PSEUDOCONSOLE_INHERIT_CURSOR|PSEUDOCONSOLE_RESIZE_QUIRK|PSEUDOCONSOLE_WIN32_INPUT_MODE,
		&PCon,
	); err != nil {
		windows.CloseHandle(stdin.Write)
		windows.CloseHandle(stdin.Read)
		windows.CloseHandle(stdout.Write)
		windows.CloseHandle(stdout.Read)
		logger.Println(err)
		return nil, err
	}
	windows.CloseHandle(stdin.Read)
	windows.CloseHandle(stdout.Write)

	return &windowsPty{
		PCon,
		size,
		&windowsReader{stdout.Read},
		stdout.Read,
		&windowsWriter{stdin.Write},
		stdin.Write,
		false,
	}, nil
}
