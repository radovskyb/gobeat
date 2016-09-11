package process

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"unicode"
	"unsafe"
)

type Process struct {
	*os.Process
	Tty  string
	Cwd  string
	Cmd  string
	Args []string
}

func (p Process) String() string {
	return fmt.Sprintf("[Pid]: %d\n"+
		"[Command]: %s\n"+
		"[Args]: %s\n"+
		"[Cwd]: %v\n"+
		"[Tty]: %s\n",
		p.Pid,
		p.Cmd,
		strings.Join(p.Args, ", "),
		p.Cwd,
		p.Tty,
	)
}

// HealthCheck signals the process to see if it's still running.
func (p Process) HealthCheck() error {
	if err := p.Signal(syscall.Signal(0)); err != nil {
		return fmt.Errorf("process is not running")
	}
	return nil
}

// Start starts a process and notifies on the notify channel
// when the process has been started. It uses stdin, stdout and
// stderr for the command's stdin, stdout and stderr respectively.
func (p *Process) Start(detach bool, stdin io.Reader, stdout, stderr io.Writer,
	notify chan<- struct{}) error {
	// Create a new command to start the process with.
	c := exec.Command(p.Cmd, p.Args...)
	c.Stdin = stdin
	c.Stdout = stdout
	c.Stderr = stderr

	if p.InTty() {
		// Start the process in a different process group if detach is set to true.
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: detach}
	} else {
		// If process didn't start in a tty and detach is true, disconnect
		// process from any tty.
		c.SysProcAttr = &syscall.SysProcAttr{Setsid: detach}
	}

	// Start the command.
	if err := c.Start(); err != nil {
		return err
	}

	notify <- struct{}{}

	// Wait for the command to finish.
	return c.Wait()
}

// StartTty starts a process in a tty and notifies on the
// notify channel when the process has been started.
func (p *Process) StartTty(ttyFd uintptr, notify chan<- struct{}) error {
	// Append a new line character to the full command so the command
	// actually executes.
	fullCommandNL := p.FullCommand() + "\n"

	// Write each byte from fullCommandNL to the tty instance.
	var eno syscall.Errno
	for _, b := range fullCommandNL {
		_, _, eno = syscall.Syscall(syscall.SYS_IOCTL,
			ttyFd,
			syscall.TIOCSTI,
			uintptr(unsafe.Pointer(&b)),
		)
		if eno != 0 {
			return error(eno)
		}
	}

	// Get the new PID of the restarted process.
	if err := p.FindPid(); err != nil {
		return err
	}

	notify <- struct{}{}

	return nil
}

// FindPid finds and then sets the a process's pid based
// on it's command, it's command's arguments and it's tty.
func (p *Process) FindPid() error {
	if p.Cmd == "" {
		return fmt.Errorf("process command is empty")
	}

	ps, err := exec.Command("ps", "-e").Output()
	if err != nil {
		log.Fatalln(err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(ps))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, p.Cmd+" "+strings.Join(p.Args, " ")) &&
			strings.Contains(line, p.Tty) {
			p.Pid, err = strconv.Atoi(strings.TrimSpace(strings.Split(line, " ")[0]))
			if err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	// Reset p.Process to the new process found from the new pid.
	p.Process, err = os.FindProcess(p.Pid)
	return err
}

func (p Process) FullCommand() string {
	return p.Cmd + " " + strings.Join(p.Args, " ")
}

func (p Process) InTty() bool {
	return p.Tty != "??"
}

func (p Process) OpenTty() (*os.File, error) {
	if !p.InTty() {
		return nil, fmt.Errorf("process is not in a tty")
	}
	return os.Open("/dev/" + p.Tty)
}

func FindByName(name string) (*Process, error) {
	psOutput, err := exec.Command("ps", "-e").Output()
	if err != nil {
		return nil, err
	}
	lowercaseOutput := bytes.ToLower(psOutput)

	var names []string
	scanner := bufio.NewScanner(bytes.NewReader(lowercaseOutput))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, name) {
			names = append(names, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Display a list of all the found names.
	for i, name := range names {
		fmt.Printf("%d: %s\n", i, name)
	}

	procNumber := -1
	fmt.Println("\nWhich number above represents the correct process (enter the number):")
	fmt.Scanf("%d", &procNumber)

	if procNumber < 0 {
		return nil, fmt.Errorf("please enter a valid number")
	}

	pid, err := strconv.Atoi(strings.TrimSpace(strings.Split(names[procNumber], " ")[0]))
	if err != nil {
		return nil, err
	}

	return FindByPid(pid)
}

func FindByPid(pid int) (*Process, error) {
	process := new(Process)

	var err error
	process.Process, err = os.FindProcess(pid)
	if err != nil {
		return nil, err
	}

	pidStr := strconv.Itoa(process.Pid)

	// Get the comm= result from ps to compare to the ps's
	// command= result to use to extract the process's args.
	//
	// ps -o comm= -p $PID
	pidCmd, err := exec.Command("ps", "-o", "comm=", pidStr).Output()
	if err != nil {
		log.Fatalln(err)
	}
	process.Cmd = strings.TrimSpace(string(pidCmd))

	// Extract process's args.
	//
	// Get the ps command= string result.
	pidCommandEq, err := exec.Command("ps", "-o", "command=", pidStr).Output()
	if err != nil {
		log.Fatalln(err)
	}

	split := strings.SplitAfter(string(pidCommandEq), process.Cmd)
	process.Args = strings.FieldsFunc(split[1], unicode.IsSpace)

	// Get the tty of the process.
	tty, err := exec.Command("ps", "-o", "tty=", "-p", pidStr).Output()
	if err != nil {
		log.Fatalln(err)
	}
	process.Tty = strings.TrimSpace(string(tty))

	// Find folder of the process (cwd).
	//
	// lsof -p $PID
	lsofOutput, err := exec.Command("lsof", "-p", pidStr).Output()
	if err != nil {
		log.Fatalln(err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(lsofOutput))
	for scanner.Scan() {
		words := strings.FieldsFunc(scanner.Text(), unicode.IsSpace)
		if words[3] == "cwd" {
			process.Cwd = strings.TrimSpace(strings.Join(words[8:], " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return process, nil
}
