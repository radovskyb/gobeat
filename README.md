# gobeat

##### So far only tested on MacOS

`gobeat`'s process functionality: https://github.com/radovskyb/process.

`gobeat` is a health check monitor, command automation tool and a process restarter, for any process found by name or pid.

### Installation:

1. `go get github.com/radovskyb/gobeat`

#### Example usage: 
`gobeat -pid=1234 -cmd="go run myscript.go"` where `1234` is the `pid` of a process that needs to be monitored and `myscript.go` is a `Go` file to be executed any time the process from `pid` `1234` is terminated or restarted.

`gobeat -name="subl" -cmd="./myshellscript"` where `subl` is a name of a process to be monitored, that `gobeat` will use to list all processes that contain that name, where the correct process will be chosen from the list and `./myshellscript` is a shell script to be executed any time the process is terminated or restarted.

#### Description:

`gobeat` is started by passing it a minimum flag that it requires to work, either a `pid` (process id) which can be found using `ps` in the terminal, or a `name` flag which finds and lists all names from the `ps` command, where the correct process name is chosen from the list.

Once running, `gobeat` uses the `interval` flag (100 milliseconds is the default), to regularly health check the process by sending it a unix 0 signal. 

If the process has shutdown or is non-respondant and the `restart` flag is set to true, which is the default, `gobeat` automatically restarts the process.
When the process that needs to be restarted was instantiated in a terminal window for example, `/dev/ttys001`, `gobeat` automatically restarts
the process in the terminal tty that it originated in. `gobeat` must be started with `sudo` to restart an application
in the correct `tty` window, otherwise it will just restart the process as a normal application, attached to `gobeat`'s process.

For processes not started from a terminal, for example, such as a text editor like `Sublime Text`,
`gobeat` will simply restart the application. The `detach` flag specifes whether or not a regular application 
that gets restarted, is attached to `gobeat`'s process. When it is attached, if `gobeat` is shut down, 
so will the restarted application. However if it's detached, which is the default, the application will 
continue running even after `gobeat` is shut down.

`gobeat` also accepts a `cmd` flag, which takes in a command that is to be run anytime the process being followed is terminated or restarted.

`gobeat` continues to check the newly created processes even after it has already been restarted by taking the `pid` from the new process and monitoring that process instead, keeping all of the same flags intact.

`gobeat` uses the unix commands `lsof` and `ps` for finding process information since `/proc/` is not available on MacOS.

##### Example workflow: `sudo gobeat -pid=1234 -cmd="go run sendemail.go"`

1. Run with `sudo` so `gobeat` will restart the server in the same terminal tty that it originated in. (`sudo`)
2. Point `gobeat` to the process of the running server that you want `gobeat` to watch. (`gobeat -pid=1234`)
3. Set the `cmd` flag to run a Go file that will send an email notifying you that the server was restarted. (`-cmd="go run sendemail.go"`)
