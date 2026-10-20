// Package tty lets a program the CLI runs see the person's terminal even when
// the CLI's own output passes through a writer that copies it to the log.
//
// Handed a writer that is not a file, os/exec gives the child a pipe, and a
// child on a pipe drops its colours, progress bars and width. Run gives it a
// pseudo-terminal instead, sized like the real one, and copies what it prints
// into the writer, so the person sees what they saw before the log existed.
package tty

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Of returns the terminal w writes to: w itself, or the terminal reached by
// following writers that pass everything through to an Underlying one.
func Of(w io.Writer) (*os.File, bool) {
	for {
		through, ok := w.(interface{ Underlying() io.Writer })
		if !ok {
			break
		}
		w = through.Underlying()
	}
	f, ok := w.(*os.File)
	if !ok || f == nil || !term.IsTerminal(int(f.Fd())) {
		return nil, false
	}
	return f, true
}

// Run runs cmd like cmd.Run, except that an output writer which is not
// itself a file but reaches a terminal is replaced, for the child, by a
// pseudo-terminal whose output is copied into that writer.
func Run(cmd *exec.Cmd) error {
	ptys := attach(cmd)
	if len(ptys) == 0 {
		return cmd.Run()
	}
	if err := cmd.Start(); err != nil {
		for _, p := range ptys {
			p.closeBoth()
		}
		return err
	}
	stopResizing := followResizes(ptys)
	defer stopResizing()

	var copying sync.WaitGroup
	for _, p := range ptys {
		_ = p.slave.Close()
		copying.Go(p.copy)
	}
	waitErr := cmd.Wait()
	drain(&copying, ptys, cmd.WaitDelay)

	if waitErr != nil {
		return waitErr
	}
	for _, p := range ptys {
		if p.writeErr != nil {
			return fmt.Errorf("copying a program's output: %w", p.writeErr)
		}
	}
	return nil
}

type terminalCopy struct {
	terminal *os.File
	master   *os.File
	slave    *os.File
	to       io.Writer
	writeErr error
}

// attach puts a pseudo-terminal in place of each writer that reaches a
// terminal. When one cannot be opened, the child keeps its writers and gets a
// pipe, as it would without a terminal.
func attach(cmd *exec.Cmd) []*terminalCopy {
	stdout, stderr := cmd.Stdout, cmd.Stderr
	var ptys []*terminalCopy
	byWriter := map[io.Writer]*terminalCopy{}
	for _, stream := range []*io.Writer{&cmd.Stdout, &cmd.Stderr} {
		w := *stream
		if w == nil {
			continue
		}
		if _, isFile := w.(*os.File); isFile {
			continue
		}
		terminal, ok := Of(w)
		if !ok {
			continue
		}
		p, seen := byWriter[w]
		if !seen {
			master, slave, err := pty.Open()
			if err != nil {
				for _, opened := range ptys {
					opened.closeBoth()
				}
				cmd.Stdout, cmd.Stderr = stdout, stderr
				return nil
			}
			p = &terminalCopy{terminal: terminal, master: master, slave: slave, to: w}
			_ = pty.InheritSize(terminal, master)
			passBytesAsWritten(slave)
			byWriter[w] = p
			ptys = append(ptys, p)
		}
		*stream = p.slave
	}
	return ptys
}

// passBytesAsWritten turns off the pseudo-terminal's own output processing,
// so a newline is not made "\r\n" twice: once here and again by the real
// terminal, which still processes what the child wrote exactly as before.
func passBytesAsWritten(slave *os.File) {
	fd := int(slave.Fd())
	termios, err := unix.IoctlGetTermios(fd, getTermios)
	if err != nil {
		return
	}
	termios.Oflag &^= unix.OPOST
	_ = unix.IoctlSetTermios(fd, setTermios, termios)
}

func (p *terminalCopy) copy() {
	buf := make([]byte, 32<<10)
	for {
		n, err := p.master.Read(buf)
		if n > 0 && p.writeErr == nil {
			if _, werr := p.to.Write(buf[:n]); werr != nil {
				p.writeErr = werr
			}
		}
		if err != nil {
			return
		}
	}
}

func (p *terminalCopy) closeBoth() {
	_ = p.master.Close()
	_ = p.slave.Close()
}

// drain waits for every copy to reach the end of what the child printed. A
// program the child left running in the background can hold the terminal
// open; after cmd.WaitDelay, as os/exec does with its pipes, the copy stops.
func drain(copying *sync.WaitGroup, ptys []*terminalCopy, delay time.Duration) {
	done := make(chan struct{})
	go func() {
		copying.Wait()
		close(done)
	}()
	var timeout <-chan time.Time
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case <-done:
	case <-timeout:
		for _, p := range ptys {
			_ = p.master.Close()
		}
		<-done
	}
	for _, p := range ptys {
		_ = p.master.Close()
	}
}

func followResizes(ptys []*terminalCopy) func() {
	resized := make(chan os.Signal, 1)
	signal.Notify(resized, syscall.SIGWINCH)
	stop := make(chan struct{})
	var stopped sync.WaitGroup
	stopped.Go(func() {
		for {
			select {
			case <-resized:
				for _, p := range ptys {
					_ = pty.InheritSize(p.terminal, p.master)
				}
			case <-stop:
				return
			}
		}
	})
	return func() {
		signal.Stop(resized)
		close(stop)
		stopped.Wait()
	}
}
