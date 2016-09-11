package process

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unicode"
)

type Process struct {
	*os.Process
	Tty  string
	Cwd  string
	Cmd  string
	Args []string
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

	// If there was a scanner error, still return whatever process information
	// has so far been retrieved.
	return process, scanner.Err()
}
