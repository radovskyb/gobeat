# gobeat

`gobeat` is a health check monitor, command automation tool and process restarter, for any process with a `pid`.

#### Example usage: 
`gobeat -pid=1234 -cmd="go run myscript.go"` where `1234` is the `pid` of a process that needs to be monitored and `myscript.go` is a `Go` file to be executed any time the process from `pid` `1234` is terminated or restarted.

#### Description:

`gobeat` is started by passing it the mimumum flag it requires to work, a `pid` (process id) found using `ps` in the terminal.

Once running, `gobeat` uses the `interval` flag (100 milliseconds is the default), to regularly `ping` 
the process by sending it a unix 0 signal. 

If the process has shutdown or is non-respondant and the `restart` flag is set to true, 
which is it's default value, `gobeat` automatically restarts the process. If the process that needs to
be restarted, was instantiated in a terminal window such as `/dev/ttys001`, `gobeat` automatically restarts
the process in the same window it originated in.

For processes not started from a terminal, for example, such as a text editor like `Sublime Text`,
`gobeat` will simply restart the application. The `detach` flag specifes whether or not a regular application 
that gets restarted, is attached to `gobeat`'s process. When it is attached, if `gobeat` is shut down, 
so will the restarted application. However if it's detached, which is the default, the application will 
continue running even if `gobeat` is shut down.

`gobeat` also accepts a `cmd` flag, which takes in a command that is to be run either anytime the process being
followed is shutdown or restarted.

`gobeat` continues to check the newly created processes even after it has already been restarted, by taking
the `pid` from the new process and monitoring that process instead, keeping all the same flags intact.

One of the main things that I find `gobeat` handy for, is for health checking a process running a webserver.
If the webserver accidently gets killed, `gobeat` automatically restarts the server with whatever command started
it in the first place with any args it was initially given.
When restarting the server's process, I have `gobeat` call a `go run` command, set using the `cmd` flag, 
that runs a simple `Go` script to send an email notifying me that the server was restarted.

It's not the most idiomatic piece of software and also uses unix commands such as `lsof`, `ps`, `grep` and `awk`,
but it gets the job done for me.

##### So far only tested on MacOS
##### Examples to come eventually
