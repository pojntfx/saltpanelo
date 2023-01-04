package utils

import (
	"io"
	"os"
	"os/exec"
	"strings"
)

func PipeShellCommand(command string, src io.Reader, dst io.Writer) error {
	if strings.TrimSpace(command) == "" {
		if _, err := io.Copy(dst, src); err != nil {
			return err
		}

		return nil
	}

	cmd := exec.Command("sh", "-c", command)
	cmd.Stdin = src
	cmd.Stdout = dst
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
