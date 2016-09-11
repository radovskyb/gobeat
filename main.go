package main

import (
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
	must(proc.HealthCheck())

	ttyFile, err := proc.OpenTty()
	if err != nil && err == os.ErrPermission {
		fmt.Println("start gobeat with sudo to restart application in correct tty")
	}
	defer ttyFile.Close()

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
			// If process was running in a tty instance, detach is set to
			// true and the user is sudo, send the command using IOCTL with
			// TIOCSTI system calls to the correct tty.
			if proc.InTty() &&
				(os.Getgid() == 0 && os.Getuid() == 0) &&
				*detach {
				// Append a new line character to the full command so the command
				// actually executes.
				fullCommandNL := proc.FullCommand() + "\n"

				// Write each byte from pidCommandEq to the tty instance.
				var eno syscall.Errno
				for _, b := range fullCommandNL {
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

				if proc.InTty() {
					// Start the process in a different process group if detach is set to true.
					c.SysProcAttr = &syscall.SysProcAttr{Setpgid: *detach}
				} else {
					// If process didn't start in a tty, don't run it as this tty.
					c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
				}

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
			err = proc.HealthCheck()
			if err != nil {
				errch <- struct{}{}
			}
		}

		// Sleep for the specified interval.
		time.Sleep(time.Millisecond * time.Duration(*interval))
	}
}

func must(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}
