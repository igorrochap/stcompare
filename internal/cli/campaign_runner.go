package cli

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

type execCampaignRunner struct{}

func (execCampaignRunner) SchemathesisVersion() (string, error) {
	command := resolveSchemathesisCommand()
	output, err := exec.Command(command.name, append(command.args, "--version")...).Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

func (execCampaignRunner) Run(argv []string) error {
	if len(argv) == 0 {
		return errors.New("schemathesis command is empty")
	}

	resolved := resolveSchemathesisCommand()
	args := argv[1:]
	if argv[0] != "st" {
		resolved = commandSpec{name: argv[0]}
	}
	command := exec.Command(resolved.name, append(resolved.args, args...)...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	err := command.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return nil
	}

	return err
}

type commandSpec struct {
	name string
	args []string
}

func resolveSchemathesisCommand() commandSpec {
	if configured := strings.Fields(os.Getenv("STCOMPARE_SCHEMATHESIS_COMMAND")); len(configured) > 0 {
		return commandSpec{name: configured[0], args: configured[1:]}
	}
	if _, err := exec.LookPath("st"); err == nil {
		return commandSpec{name: "st"}
	}
	if _, err := exec.LookPath("uvx"); err == nil {
		return commandSpec{name: "uvx", args: []string{"schemathesis"}}
	}

	return commandSpec{name: "st"}
}
