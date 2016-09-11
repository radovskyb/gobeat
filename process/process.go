package process

type Process struct {
	Pid  int
	Tty  string
	Cwd  string
	Cmd  string
	Args []string
}
