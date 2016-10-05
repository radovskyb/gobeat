# gobeat
`gobeat` is a health check monitor, command automation tool and a process restarter, for any process found by name or pid.

# Installation

```shell
go get -u github.com/radovskyb/gobeat
```

# Usage

```shell 
gobeat -pid=1234 -cmd="go run myscript.go"
```

```shell 
gobeat -name="subl" -cmd="./mybashscript"
```

# Example

```shell
sudo gobeat -pid=1234 -cmd="go run sendemail.go"
```

1. Run with `sudo` so `gobeat` will restart the server in the same terminal tty that it originated in. (`sudo`)
2. Point `gobeat` to the process of the running server that you want `gobeat` to monitor. (`gobeat -pid=1234`)
3. Set the `cmd` flag to run a `Go` file that will send an email notifying you that the server was restarted. (`-cmd="go run sendemail.go"`)

# Todo

- Make health check poll interval customizable.
- Write some tests.

# Description

`gobeat` is started by passing it a minimum flag that it requires to work, either a `pid` (process id) which can be found using `ps` in the terminal, or a `name` which finds and lists all processes containing that name, where the correct process is then chosen from the list.

Once running, `gobeat` regularly health checks the running process.

If the process has been shut down and the `restart` flag is set to true, which is the default, `gobeat` automatically restarts the process.

When the process that needs to be restarted was instantiated in a terminal window, for example, `/dev/ttys001`, `gobeat` automatically restarts
the process in the `tty` that it originated in (requires sudo). 

For processes not started in a terminal, `gobeat` will simply restart the application.

The `detach` flag specifes whether or not an application that is restarted gets attached to `gobeat`'s process. 

An `attached` process will be killed when `gobeat` is killed, whereas a `deatched` process will continue to run.

`gobeat` also accepts a `cmd` flag, which takes in a command that is to be run anytime the process being followed is terminated or restarted.

`gobeat` uses `lsof` and `ps` for finding process information since `/proc/` is not available on MacOS.
