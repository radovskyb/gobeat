package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/radovskyb/process"
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
		// Find the process by name.
		//
		// Output the possible names list to os.Stdout and scan the number
		// to use to choose the correct name from os.Stdin.
		proc, err = process.FindByName(os.Stdout, os.Stdin, *procName)
		if err != nil {
			log.Fatalln(err)
		}
	}

	// Check initial heartbeat.
	must(proc.HealthCheck())

	// Make sure gobeat is running as sudo if user wants to restart process in a tty.
	var ttyFile *os.File
	if proc.InTty() && *detach {
		if os.Getgid() != 0 || os.Getuid() != 0 {
			fmt.Println("gobeat needs to be started with sudo to restart the process in it's tty.\n" +
				"either start gobeat with sudo OR with -detach=false to restart with gobeat's process.")
			os.Exit(0)
		} else {
			ttyFile, err = proc.OpenTty()
			if err != nil {
				log.Fatalln(err)
			}
			defer ttyFile.Close()
		}
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

			// Set running to 1 so the process signal isn't re-sent until the
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
			if err := proc.Chdir(); err != nil {
				// If the folder DOES exist, report the error, otherwise,
				// just run the process from the current folder if possible.
				if os.IsExist(err) {
					return
				}
			}

			// Restart the process.
			//
			// If process was running in a tty instance, detach is set to
			// true and the user is sudo, send the command using IOCTL with
			// TIOCSTI system calls to the correct tty.
			if proc.InTty() && *detach {
				must(proc.StartTty(ttyFile.Fd(), restarted))
			} else {
				must(proc.Start(*detach, os.Stdin, os.Stdout, os.Stderr, restarted))
			}

			// Set running back to 0 so the process signal can be re-sent again.
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
