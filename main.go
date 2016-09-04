package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
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

	// If restart is set to true, find the command that started the process.
	var pidStr = strconv.Itoa(*pid)

	// ps -o comm= -p $PID
	pidCmd, err := exec.Command("ps", "-o", "comm=", pidStr).Output()
	if err != nil {
		log.Fatalln(err)
	}

	errch := make(chan struct{})
	go func() {
		for {
			<-errch

			if *cmd != "" {
				command := strings.Split(*cmd, " ")

				c := exec.Command(command[0])
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

			// Restart the process.
			pidCmdStr := strings.Trim(string(pidCmd), "\n\r")
			if err := exec.Command(pidCmdStr).Run(); err != nil {
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
