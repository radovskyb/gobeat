package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/radovskyb/gobeat/process"
)

func main() {
	pid := flag.Int("pid", -1, "the pid of the process to follow")
	interval := flag.Int("interval", 100, "interval for health checking the process in milliseconds")
	cmd := flag.String("cmd", "", "run a command any time the process restarts or terminates")
	restart := flag.Bool("restart", true, "restart the process on any time it terminates")
	detach := flag.Bool("detach", true, "detach the restarted process group from gobeat")
	procName := flag.String("name", "", "the name of the process to find a pid for")

	flag.Parse()

	if *pid == -1 && *procName == "" {
		log.Fatalf("pid or name flag not specified")
	}

	// Find the process associated with the pid.
	//
	// Check if a pid was supplied, otherwise check the name flag.
	var err error
	var proc *process.Process
	if *pid != -1 {
		proc, err = process.FindByPid(*pid)
		if err != nil {
			log.Fatalln(err)
		}
	} else {
		proc, err = process.FindByName(*procName)
		if err != nil {
			log.Fatalln(err)
		}
	}

	// Check initial heartbeat.
	err = proc.Signal(syscall.Signal(0))
	if err != nil {
		log.Fatalln("process is not running")
	}

	var ttyFile *os.File
	if proc.Tty != "??" {
		// Open the tty file.
		ttyFile, err = os.Open("/dev/" + proc.Tty)

		// Check for sudo privileges and any errors.
		if err != nil || (os.Getuid() != 0 && os.Getgid() != 0) {
			// If we can't open /dev/{ttyStr}, continue as if it's
			// a regular application not in a tty.
			proc.Tty = "??"
		}

		// Defer to close ttyFile.
		defer ttyFile.Close()
	}

	// Log the process's information
	fmt.Print(proc)

	// running hold a 1 or a 0 depending on whether or not the process has completed
	// it's restart process yet or not.
	var running int64

	errch, restarted := make(chan struct{}), make(chan struct{})
	go func() {
		for {
			<-errch

			// Set running to 1 so the proces signal isn't re-sent until the
			// of running cmd and/or restarting the specified process completes.
			atomic.AddInt64(&running, 1)

			if *cmd != "" {
				command := strings.Split(*cmd, " ")

				c := exec.Command(command[0])
				if len(command) > 1 {
					c.Args = append(c.Args, command[1:]...)
				}
				c.Stdin = os.Stdin
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr

				// Start the command.
				must(c.Start())

				// Print a running command message.
				fmt.Printf("\n[Running command]: %s\n", *cmd)

				// Wait for the command to finish.
				must(c.Wait())
			}

			// If restart is not set to true, exit cleanly.
			if *restart != true {
				os.Exit(0)
			}

			// Change into the working directory where the process was called.
			if err := os.Chdir(proc.Cwd); err != nil {
				// If the folder DOES exist, report the error, otherwise,
				// just run the process from the current folder if possible.
				if os.IsExist(err) {
					log.Fatalln(err)
				}
			}

			// Restart the process.
			//
			// If process was running in a tty instance and detach is set to
			// true, send the command using IOCTL with TIOCSTI system calls.
			if proc.Tty != "??" {
				// Append a new line character to the full command so the command
				// actually executes.
				pidCommandEqNL := proc.Cmd + " " + strings.Join(proc.Args, " ") + "\n"

				// Write each byte from pidCommandEq to the tty instance.
				var eno syscall.Errno
				for _, b := range pidCommandEqNL {
					_, _, eno = syscall.Syscall(syscall.SYS_IOCTL,
						ttyFile.Fd(),
						syscall.TIOCSTI,
						uintptr(unsafe.Pointer(&b)),
					)
					if eno != 0 {
						log.Fatalln(eno)
					}
				}

				// Get the new PID of the restarted process.
				if err := proc.FindPid(); err != nil {
					log.Fatalln(err)
				}

				restarted <- struct{}{}
			} else {
				// Create a new command to start the process with.
				c := exec.Command(proc.Cmd, proc.Args...)
				c.Stdin = os.Stdin
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr

				// Start the process in a different process group if detach
				// is set to true.
				c.SysProcAttr = &syscall.SysProcAttr{Setpgid: *detach}

				// Start the command.
				must(c.Start())

				restarted <- struct{}{}

				// Wait for the command to finish.
				must(c.Wait())
			}

			// Set running back to 0 so the proces signal can be re-sent again.
			atomic.AddInt64(&running, -1)
		}
	}()

	go func() {
		for {
			// Any time the restarted channel is received from, print a
			// message saying what process was restarted.
			<-restarted

			// Print a restarted message.
			fmt.Printf("\n[Restarted]: %s\n", proc.Cmd)
		}
	}()

	for {
		// Send a signal if no restart is already in progress.
		if atomic.LoadInt64(&running) == 0 {
			// Check if the process is running.
			err := proc.Signal(syscall.Signal(0))
			if err != nil {
				errch <- struct{}{}
			}
		}

		// Sleep for the specified interval.
		time.Sleep(time.Millisecond * time.Duration(*interval))
	}
}

// getPidByName takes in a name and finds the pid associated with it.
func getPidByName(procName string) (string, error) {
	// ps -o | grep -i "name" | grep -v grep
	psOutput, err := exec.Command("ps", "-e").Output()
	if err != nil {
		log.Fatalln(err)
	}
	lowercaseOutput := bytes.ToLower(psOutput)

	var names []string
	scanner := bufio.NewScanner(bytes.NewReader(lowercaseOutput))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, procName) {
			names = append(names, line)
		}
	}
	must(scanner.Err())

	// Display a list of all the found names.
	for i, name := range names {
		fmt.Printf("%d: %s\n", i, name)
	}

	procNumber := -1
	fmt.Println("\nWhich number above represents the correct process (enter the number):")
	fmt.Scanf("%d", &procNumber)

	if procNumber < 0 {
		return "", fmt.Errorf("please enter a valid number")
	}

	// Return the pid string.
	return strings.TrimSpace(strings.Split(names[procNumber], " ")[0]), nil
}

func must(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}
