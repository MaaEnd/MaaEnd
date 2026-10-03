package startgame

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func configureCommand(cmd *exec.Cmd) {
	// Keep the application independent of the pretask's console and Ctrl+C group.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
}

func parseArguments(arguments string) ([]string, error) {
	// Windows treats argv[0] differently, so prepend a placeholder executable.
	args, err := windows.DecomposeCommandLine("program " + arguments)
	if err != nil {
		return nil, err
	}
	return args[1:], nil
}
