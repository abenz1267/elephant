package common

import (
	"os/exec"
	"strings"
)

func ReplaceResultOrStdinCmd(replace, result string) *exec.Cmd {
	if !strings.Contains(replace, "%RESULT%") {
		cmd := exec.Command("sh", "-c", replace)

		if !strings.HasSuffix(result, "\n") {
			result += "\n"
		}

		cmd.Stdin = strings.NewReader(result)
		return cmd
	}

	return exec.Command("sh", "-c", strings.ReplaceAll(replace, "%RESULT%", result))
}
