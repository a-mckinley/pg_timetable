package scheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

type commander interface {
	CombinedOutput(string, ...string) ([]byte, error)
}

type realCommander struct{}

func (c realCommander) CombinedOutput(command string, args ...string) ([]byte, error) {
	return exec.Command(command, args...).CombinedOutput()
}

var cmd commander

//ExecuteShellCommand executes built-in task depending on task name and returns err result
func ExecuteShellCommand(command string, paramValues []string) (int, error) {
	if strings.TrimSpace(command) == "" {
		return -1, errors.New("Shell command cannot be empty")
	}
	if len(paramValues) == 0 { //mimic empty param
		paramValues = []string{""}
	}
	for _, val := range paramValues {
		params := []string{}
		if val > "" {
			if err := json.Unmarshal([]byte(val), &params); err != nil {
				return -1, err
			}
		}
		out, err := cmd.CombinedOutput(command, params...) // #nosec
		cmdLine := fmt.Sprintf("%s %v:\n", command, params)
		pgengine.LogToDB("DEBUG", "Output for command ", cmdLine, string(out))
		if err != nil {
			//check if we're dealing with an ExitError - i.e. return code other than 0
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode := exitError.ProcessState.ExitCode()
				pgengine.LogToDB("DEBUG", "Return value of the command ", cmdLine, exitCode)
				return exitCode, exitError
			}
			return -1, err
		}
	}
	return 0, nil
}

func init() {
	cmd = realCommander{}
}
