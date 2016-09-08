package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"
	"unsafe"
)

func main() {
	pid := flag.Int("pid", -1, "process pid to follow")
	interval := flag.Int("interval", 100, "interval for checking the process in milliseconds")
	cmd := flag.String("cmd", "", "run a command when the process terminates")
	restart := flag.Bool("restart", true, "restart the process on termination")
	detach := flag.Bool("detach", true, "detach the restarted process group")

	flag.Parse()

	if *pid == -1 {
		log.Fatalf("pid flag not specified")
	}

	// Find the process associated with the pid.
	proc, err := os.FindProcess(*pid)
	if err != nil {
		log.Fatalln(err)
	}

	// Check initial heartbeat.
	err = proc.Signal(syscall.Signal(0))
	if err != nil {
		log.Fatalln("process is not running")
	}

	pidIntStr := strconv.Itoa(*pid)

	// If restart is set to true, find the command that started the process.
	//
	// ps -o comm= -p $PID
	pidCmd, err := exec.Command("ps", "-o", "comm=", pidIntStr).Output()
	if err != nil {
		log.Fatalln(err)
	}

	pidCmdStr := strings.Trim(string(pidCmd), "\n\r")

	// Extract args. Example vim main.go
	//
	// Get the ps command= string result.
	pidCommandEq, err := exec.Command("ps", "-o", "command=", pidIntStr).Output()
	if err != nil {
		log.Fatalln(err)
	}

	split := strings.SplitAfter(string(pidCommandEq), string(pidCmdStr))
	args := strings.FieldsFunc(split[1], unicode.IsSpace)

	// Find tty of process if one is available
	//
	// ps -o tty= -p $PID
	tty, err := exec.Command("ps", "-o", "tty=", "-p", pidIntStr).Output()
	if err != nil {
		log.Fatalln(err)
	}
	ttyStr := strings.Trim(string(tty), "\n\r ")

	var ttyFile *os.File

	if ttyStr != "??" {
		// Open the tty file.
		ttyFile, err = os.Open("/dev/" + ttyStr)
		if err != nil {
			log.Fatalln(err)
		}
		defer ttyFile.Close()

		if os.Getgid() != 0 && os.Getuid() != 0 {
			log.Fatalln("gobeat needs to run with sudo for restarting this process")
		}
	}

	// Find folder of running process.
	//
	// lsof -p $PID | awk '$4=="cwd" {print $9}'
	output, err := exec.Command("lsof", "-p", pidIntStr).Output()
	if err != nil {
		log.Fatalln(err)
	}
	c := exec.Command("awk", "$4==\"cwd\" {print $9}")
	c.Stdin = bytes.NewReader(output)
	c.Stderr = os.Stderr
	folderName, err := c.Output()
	if err != nil {
		log.Fatalln(err)
	}
	folderNameStr := strings.Trim(string(folderName), "\n\r")

	fmt.Printf("[Process Folder]: %s\n[Command]: %s\n[Args]: %v\n",
		folderNameStr, pidCmdStr, strings.Join(args, ", "))

	// running hold a 1 or a 0 depending on whether or not the process has completed
	// it's restart process yet or not.
	var running int64

	errch := make(chan struct{})
	go func() {
		for {
			<-errch

			atomic.AddInt64(&running, 1)

			if *cmd != "" {
				command := strings.Split(*cmd, " ")

				if len(command) > 1 {
					c = exec.Command(command[0], command[1:]...)
				} else {
					c = exec.Command(command[0])
				}
				c.Stdin = os.Stdin
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr

				if err := c.Run(); err != nil {
					log.Fatalln(err)
				}
			}

			// If restart is not set to true, exit cleanly.
			if *restart != true {
				os.Exit(0)
			}

			// Change into the working directory where the process was called.
			if err := os.Chdir(folderNameStr); err != nil {
				// If the folder DOES exist, report the error, otherwise,
				// just run the process from the current folder if possible.
				if os.IsExist(err) {
					log.Fatalln(err)
				}
			}

			// Restart the process.

			// If process was running in a tty instance send the command
			// using IOCTL with TIOCSTI system calls.
			if ttyStr != "??" {
				// Append a new line character to cmdBytes so the command
				// actually executes.
				pidCommandEqNL := append(pidCommandEq, byte('\n'))

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
				//
				// ps -e | grep ttys002 | grep 'vim main.go' | awk '{print $1}'
				psOutput, err := exec.Command("ps", "-e").Output()
				if err != nil {
					log.Fatalln(err)
				}
				grepCmd1 := exec.Command("grep", ttyStr)
				grepCmd1.Stdin = bytes.NewReader(psOutput)
				grepCmd1.Stderr = os.Stderr
				grepOutput1, err := grepCmd1.Output()
				if err != nil {
					log.Fatalln(err)
				}
				grepCmd2 := exec.Command("grep", strings.Trim(string(pidCommandEq), "\n\r "))
				grepCmd2.Stdin = bytes.NewReader(grepOutput1)
				grepCmd2.Stderr = os.Stderr
				grepOutput2, err := grepCmd2.Output()
				if err != nil {
					log.Fatalln(err)
				}
				awkCmd := exec.Command("awk", "{print $1}")
				awkCmd.Stdin = bytes.NewReader(grepOutput2)
				awkCmd.Stderr = os.Stderr
				awkOutput, err := awkCmd.Output()
				if err != nil {
					log.Fatalln(err)
				}
				pid, err := strconv.Atoi(strings.Trim(string(awkOutput), "\n\r "))
				if err != nil {
					log.Fatalln(err)
				}
				// Reset proc to the new process found from the new pid.
				proc, err = os.FindProcess(pid)
				if err != nil {
					log.Fatalln(err)
				}
			} else {
				// Create a new command to start the process with.
				c = exec.Command(pidCmdStr, args...)
				c.Stdin = os.Stdin
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr

				// Start the process in a different process group if detach
				// is set to true.
				c.SysProcAttr = &syscall.SysProcAttr{Setpgid: *detach}

				// Run the command.
				if err := c.Run(); err != nil {
					log.Fatalln(err)
				}
			}

			atomic.AddInt64(&running, -1)
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
