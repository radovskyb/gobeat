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
	"syscall"
	"time"
	"unicode"
)

func main() {
	pid := flag.Int("pid", -1, "process pid to follow")
	interval := flag.Int("interval", 100, "interval for checking the process (milliseconds)")
	cmd := flag.String("cmd", "", "run a command when the process terminates")
	restart := flag.Bool("restart", true, "restart the process on termination")

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

	// Find folder of running process.
	//
	// lsof -a -d cwd -p $PID | awk '$4=="cwd" {print $9}'
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

	errch := make(chan struct{})
	go func() {
		for {
			<-errch

			if *cmd != "" {
				command := strings.Split(*cmd, " ")

				c = exec.Command(command[0])
				c.Stdin = os.Stdin
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr

				if len(command) > 1 {
					c.Args = command[1:]
				}

				if err := c.Run(); err != nil {
					log.Fatalln(err)
				}
			}

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
			c = exec.Command(pidCmdStr, args...)
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr

			if err := c.Run(); err != nil {
				log.Fatalln(err)
			}
		}
	}()

	for {
		// Check if the process is running.
		err := proc.Signal(syscall.Signal(0))
		if err != nil {
			errch <- struct{}{}
		}

		// Sleep for the specified interval.
		time.Sleep(time.Millisecond * time.Duration(*interval))
	}
}
