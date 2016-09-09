package main

import (
	"bufio"
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

var err error
var stdout bytes.Buffer

func main() {
	pid := flag.Int("pid", -1, "process pid to follow")
	interval := flag.Int("interval", 100, "interval for checking the process in milliseconds")
	cmd := flag.String("cmd", "", "run a command when the process terminates")
	restart := flag.Bool("restart", true, "restart the process on termination")
	detach := flag.Bool("detach", true, "detach the restarted process group")
	procName := flag.String("name", "", "the name of the process to find a pid for")

	flag.Parse()

	if *pid == -1 && *procName == "" {
		log.Fatalf("pid or name flag not specified")
	}

	// Check if a pid was supplied, otherwise check the name flag.
	var pidIntStr string
	var pidInt int
	if *pid != -1 {
		pidIntStr = strconv.Itoa(*pid)
		pidInt = *pid
	} else {
		pidIntStr, err = getPidByName(*procName)
		if err != nil {
			log.Fatalln(err)
		}
		pidInt, err = strconv.Atoi(pidIntStr)
		if err != nil {
			log.Fatalln(err)
		}
	}

	// Find the process associated with the pid.
	proc, err := os.FindProcess(pidInt)
	if err != nil {
		log.Fatalln(err)
	}

	// Check initial heartbeat.
	err = proc.Signal(syscall.Signal(0))
	if err != nil {
		log.Fatalln("process is not running")
	}

	// If restart is set to true, find the command that started the process.
	//
	// ps -o comm= -p $PID
	pidCmd, err := exec.Command("ps", "-o", "comm=", pidIntStr).Output()
	if err != nil {
		log.Fatalln(err)
	}

	pidCmdStr := trimOutput(pidCmd)

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
	var ttyStr = "??"

	if *detach {
		tty, err := exec.Command("ps", "-o", "tty=", "-p", pidIntStr).Output()
		if err != nil {
			log.Fatalln(err)
		}
		ttyStr = trimOutput(tty)
	}

	var ttyFile *os.File
	if ttyStr != "??" {
		// Open the tty file.
		ttyFile, err = os.Open("/dev/" + ttyStr)

		// Check for sudo privileges and any errors.
		if err != nil || (os.Getuid() != 0 && os.Getgid() != 0) {
			// If we can't open /dev/{ttyStr}, continue as if it's
			// a regular application not in a tty.
			ttyStr = "??"
		}

		// Defer to close ttyFile.
		defer ttyFile.Close()
	}

	// Find folder of running process.
	//
	// lsof -p $PID | awk '$4=="cwd" {print $9}'
	lsofOutput, err := exec.Command("lsof", "-p", pidIntStr).Output()
	if err != nil {
		log.Fatalln(err)
	}

	// Replacement for above awk.
	var folderName string
	scanner := bufio.NewScanner(bytes.NewReader(lsofOutput))
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		words := strings.FieldsFunc(scanner.Text(), unicode.IsSpace)
		if words[3] == "cwd" {
			folderName = strings.Join(words[8:], " ")
		}
	}

	folderNameStr := trimOutput([]byte(folderName))
	fmt.Println(folderNameStr)

	fmt.Printf("[Process Folder]: %s\n[Command]: %s\n[Args]: %v\n",
		folderNameStr, pidCmdStr, strings.Join(args, ", "))

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
			if err := os.Chdir(folderNameStr); err != nil {
				// If the folder DOES exist, report the error, otherwise,
				// just run the process from the current folder if possible.
				if os.IsExist(err) {
					log.Fatalln(err)
				}
			}

			// Restart the process.

			// If process was running in a tty instance and detach is set to
			// true, send the command using IOCTL with TIOCSTI system calls.
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
				ps := exec.Command("ps", "-e")
				grep1 := exec.Command("grep", ttyStr)
				grep1.Stdin, err = ps.StdoutPipe()
				if err != nil {
					log.Fatalln(err)
				}
				grep2 := exec.Command("grep", trimOutput(pidCommandEq))
				grep2.Stdin, err = grep1.StdoutPipe()
				if err != nil {
					log.Fatalln(err)
				}
				stdout.Reset()
				grep2.Stdout = &stdout

				must(grep2.Start())
				must(grep1.Start())
				must(ps.Run())
				must(grep1.Wait())
				must(grep2.Wait())

				pidStr := strings.Split(stdout.String(), " ")[0]

				pid, err := strconv.Atoi(trimOutput([]byte(pidStr)))
				if err != nil {
					log.Fatalln(err)
				}

				// Reset proc to the new process found from the new pid.
				proc, err = os.FindProcess(pid)
				if err != nil {
					log.Fatalln(err)
				}

				restarted <- struct{}{}
			} else {
				// Create a new command to start the process with.
				c := exec.Command(pidCmdStr, args...)
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
			fmt.Printf("\n[Restarted]: %s\n", pidCmdStr)
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
	// ps -o command= -e | grep -i "name" | grep -v grep
	ps := exec.Command("ps", "-o", "command=", "-e")
	grep1 := exec.Command("grep", "-i", procName)
	grep1.Stdin, err = ps.StdoutPipe()
	if err != nil {
		return "", err
	}
	grep2 := exec.Command("grep", "-v", "grep")
	grep2.Stdin, err = grep1.StdoutPipe()
	if err != nil {
		return "", err
	}
	stdout.Reset()
	grep2.Stdout = &stdout

	must(grep2.Start())
	must(grep1.Start())
	must(ps.Run())
	must(grep1.Wait())
	must(grep2.Wait())

	// Display a list of all the found names.
	names := strings.Split(trimOutput(stdout.Bytes()), "\n")
	for i, name := range names {
		fmt.Printf("%d: %s\n", i, name)
	}

	procNumber := -1
	fmt.Println("\nWhich number above represents the correct process (enter the number):")
	fmt.Scanf("%d", &procNumber)

	if procNumber < 0 {
		return "", fmt.Errorf("please enter a valid number")
	}

	// Get the pid for the process.
	//
	// ps -e | grep "name" | grep -v grep | awk '{print $1}'
	ps = exec.Command("ps", "-e")
	grep1 = exec.Command("grep", names[procNumber])
	grep1.Stdin, err = ps.StdoutPipe()
	if err != nil {
		log.Fatalln(err)
	}
	grep2 = exec.Command("grep", "-v", "grep")
	grep2.Stdin, err = grep1.StdoutPipe()
	if err != nil {
		log.Fatalln(err)
	}
	stdout.Reset()
	grep2.Stdout = &stdout

	must(grep2.Start())
	must(grep1.Start())
	must(ps.Run())
	must(grep1.Wait())
	must(grep2.Wait())

	// Return the pid string.
	return trimOutput([]byte(strings.Split(stdout.String(), " ")[0])), nil
}

func trimOutput(output []byte) string {
	return strings.Trim(string(output), "\n\r ")
}

func must(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}
