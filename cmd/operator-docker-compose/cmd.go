package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/go-orb/go-orb/codecs"
	"github.com/go-orb/go-orb/config"
	"github.com/go-orb/go-orb/log"
	"github.com/urfave/cli/v3"
)

type composeFilePathKey struct{}
type composeCommandKey struct{}
type loggerKey struct{}

func readConfig(logger log.Logger) (map[string]any, error) {
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		logger.Error("Error while reading stdin", "error", err)
		return nil, fmt.Errorf("while reading stdin: %w", err)
	}

	codec, err := codecs.GetMime(codecs.MimeJSON)
	if err != nil {
		logger.Error("Error while getting codec", "error", err)
		return nil, fmt.Errorf("while getting codec: %w", err)
	}

	var data map[string]any
	if err := codec.Unmarshal(b, &data); err != nil {
		logger.Error("Error while unmarshalling", "error", err)
		return nil, fmt.Errorf("while unmarshalling: %w", err)
	}

	return data, nil
}

func prepareConfig(logger log.Logger, data map[string]any) (map[string]any, error) {
	delete(data, "configs")
	delete(data, "octoctl")
	delete(data, "repos")

	projectID := data["projectID"].(string)
	delete(data, "projectID")
	data["name"] = projectID

	services, ok := data["services"].(map[string]any)
	if !ok {
		logger.Error("services not found")
		return nil, errors.New("services not found")
	}

	for name := range services {
		delete(services[name].(map[string]any), "octocompose")
	}

	return data, nil
}

func writeConfig(logger log.Logger, data map[string]any, projectID string) (string, error) {
	codec, err := codecs.GetMime(codecs.MimeYAML)
	if err != nil {
		logger.Error("Error while getting codec", "error", err)
		return "", fmt.Errorf("while getting codec: %w", err)
	}

	b, err := codec.Marshal(data)
	if err != nil {
		logger.Error("Error while marshalling", "error", err)
		return "", fmt.Errorf("while marshalling: %w", err)
	}

	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		logger.Error("Error while getting cache directory", "error", err)
		return "", fmt.Errorf("while getting cache directory: %w", err)
	}

	composeFilePath := filepath.Join(userCacheDir, "octocompose", projectID, "compose.yaml")
	if err := os.MkdirAll(filepath.Dir(composeFilePath), 0700); err != nil {
		logger.Error("Error while creating the cache directory", "error", err)
		return "", fmt.Errorf("while creating the cache directory: %w", err)
	}

	// Remove existing file.
	if _, err := os.Stat(composeFilePath); err == nil {
		if err := os.Remove(composeFilePath); err != nil {
			return "", fmt.Errorf("while removing file '%s': %w", composeFilePath, err)
		}
	}

	if err := os.WriteFile(composeFilePath, b, 0600); err != nil {
		logger.Error("Error while writing file", "error", err)
		return "", fmt.Errorf("while writing file: %w", err)
	}

	return composeFilePath, nil
}

func beforeConfig(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	logger, err := log.New(log.WithLevel(cmd.String("log-level")))
	if err != nil {
		return ctx, err
	}

	ctx = context.WithValue(ctx, loggerKey{}, logger)

	data, err := readConfig(logger)
	if err != nil {
		logger.Error("Error while reading config", "error", err)
		os.Exit(1)
	}

	projectID := data["projectID"].(string)
	composeCommand := []string{"docker", "compose"}

	tmpCmd := []string{}
	if err := config.ParseSlice([]string{"octoctl"}, "command", data, &tmpCmd); err == nil {
		composeCommand = tmpCmd
	}

	data, err = prepareConfig(logger, data)
	if err != nil {
		logger.Error("Error while reading and preparing config", "error", err)
		os.Exit(1)
	}

	composeFilePath, err := writeConfig(logger, data, projectID)
	if err != nil {
		logger.Error("Error while writing config", "error", err)
		os.Exit(1)
	}

	ctx = context.WithValue(ctx, composeFilePathKey{}, composeFilePath)
	ctx = context.WithValue(ctx, composeCommandKey{}, composeCommand)

	return ctx, nil
}

var startCmd = &cli.Command{
	Name:  "start",
	Usage: "run docker compose up -d",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "dry-run",
		},
	},
	Before: beforeConfig,
	Action: func(ctx context.Context, cmd *cli.Command) error {
		composeFilePath := ctx.Value(composeFilePathKey{}).(string)
		composeCommand := ctx.Value(composeCommandKey{}).([]string)

		args := append(composeCommand, []string{"-f", composeFilePath, "up", "-d"}...)
		if cmd.Bool("dry-run") {
			args = append(args, "--dry-run")
		}

		logger := ctx.Value(loggerKey{}).(log.Logger)
		logger.Debug("Running", "command", args[0], "args", args[1:])

		execCmd := exec.Command(args[0], args[1:]...)
		execCmd.Stdout = os.Stdout
		execCmd.Stderr = os.Stderr
		if err := execCmd.Run(); err != nil {
			os.Exit(execCmd.ProcessState.ExitCode())
		}

		return nil
	},
}

var stopCmd = &cli.Command{
	Name:  "stop",
	Usage: "run docker compose down",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "dry-run",
		},
	},
	Before: beforeConfig,
	Action: func(ctx context.Context, cmd *cli.Command) error {
		composeFilePath := ctx.Value(composeFilePathKey{}).(string)
		composeCommand := ctx.Value(composeCommandKey{}).([]string)

		args := append(composeCommand, []string{"-f", composeFilePath, "down"}...)
		if cmd.Bool("dry-run") {
			args = append(args, "--dry-run")
		}

		logger := ctx.Value(loggerKey{}).(log.Logger)
		logger.Debug("Running", "command", args[0], "args", args[1:])

		execCmd := exec.Command(args[0], args[1:]...)
		execCmd.Stdout = os.Stdout
		execCmd.Stderr = os.Stderr
		if err := execCmd.Run(); err != nil {
			os.Exit(execCmd.ProcessState.ExitCode())
		}

		return nil
	},
}

var logCmd = &cli.Command{
	Name:  "log",
	Usage: "run docker compose logs -f",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "follow",
			Aliases: []string{"f"},
			Usage:   "Follow the logs.",
		},
	},
	Before: beforeConfig,
	Action: func(ctx context.Context, cmd *cli.Command) error {
		composeFilePath := ctx.Value(composeFilePathKey{}).(string)
		composeCommand := ctx.Value(composeCommandKey{}).([]string)

		args := append(composeCommand, []string{"-f", composeFilePath, "logs"}...)
		if cmd.Bool("follow") {
			args = append(args, "-f")
		}

		logger := ctx.Value(loggerKey{}).(log.Logger)
		logger.Debug("Running", "command", args[0], "args", args[1:])

		execCmd := exec.Command(args[0], args[1:]...)
		execCmd.Stdout = os.Stdout
		execCmd.Stderr = os.Stderr
		if err := execCmd.Run(); err != nil {
			os.Exit(execCmd.ProcessState.ExitCode())
		}

		return nil
	},
}
