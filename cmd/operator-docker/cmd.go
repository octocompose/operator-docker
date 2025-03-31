package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/go-orb/go-orb/codecs"
	"github.com/go-orb/go-orb/config"
	"github.com/go-orb/go-orb/log"
	"github.com/urfave/cli/v3"

	"github.com/octocompose/octoctl/pkg/octoconfig"
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
	repo := octoconfig.Repo{}
	if err := config.Parse(nil, "repos", data, &repo); err != nil {
		logger.Error("Error while parsing config", "error", err)
		return nil, fmt.Errorf("while parsing config: %w", err)
	}

	delete(data, "configs")
	delete(data, "octoctl")
	delete(data, "repos")

	services, ok := data["services"].(map[string]any)
	if !ok {
		logger.Error("services not found")
		return nil, errors.New("services not found")
	}

	for name := range services {
		svc := services[name].(map[string]any)

		// Remove disabled services
		if svc["enabled"] != nil && !svc["enabled"].(bool) {
			delete(services, name)
			continue
		}

		delete(svc, "octocompose")

		if svcRepo, ok := repo.Services[name]; ok && svcRepo.Docker != nil {
			svc["image"] = svcRepo.Docker.Registry + "/" + svcRepo.Docker.Image + ":" + svcRepo.Docker.Tag

			if svcRepo.Docker.Command != nil {
				svc["command"] = svcRepo.Docker.Command
			}

			if svcRepo.Docker.Entrypoint != "" {
				svc["entrypoint"] = svcRepo.Docker.Entrypoint
			}
		} else {
			delete(services, name)
		}
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

	configData, err := readConfig(logger)
	if err != nil {
		logger.Error("Error while reading config", "error", err)
		os.Exit(1)
	}

	projectID := configData["name"].(string)
	composeCommand := []string{"docker", "compose"}

	tmpCmd := []string{}
	if err := config.ParseSlice([]string{"octoctl"}, "command", configData, &tmpCmd); err == nil {
		composeCommand = tmpCmd
	}

	configData, err = prepareConfig(logger, configData)
	if err != nil {
		logger.Error("Error while reading and preparing config", "error", err)
		os.Exit(1)
	}

	composeFilePath, err := writeConfig(logger, configData, projectID)
	if err != nil {
		logger.Error("Error while writing config", "error", err)
		os.Exit(1)
	}

	ctx = context.WithValue(ctx, composeFilePathKey{}, composeFilePath)
	ctx = context.WithValue(ctx, composeCommandKey{}, composeCommand)

	return ctx, nil
}

func runCmd(ctx context.Context, args []string) error {
	logger := ctx.Value(loggerKey{}).(log.Logger)
	logger.Debug("Running", "command", args[0], "args", args[1:])

	execCmd := exec.Command(args[0], args[1:]...)
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	if err := execCmd.Run(); err != nil {
		os.Exit(execCmd.ProcessState.ExitCode())
	}

	return nil
}

func runCompose(ctx context.Context, args []string) error {
	composeFilePath := ctx.Value(composeFilePathKey{}).(string)
	composeCommand := ctx.Value(composeCommandKey{}).([]string)

	args2 := append(composeCommand, []string{"-f", composeFilePath}...)
	args2 = append(args2, args...)

	return runCmd(ctx, args2)
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
		if cmd.Bool("dry-run") {
			return runCompose(ctx, []string{"up", "-d", "--dry-run"})
		}

		return runCompose(ctx, []string{"up", "-d"})
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
		if cmd.Bool("dry-run") {
			return runCompose(ctx, []string{"down", "--dry-run"})
		}

		return runCompose(ctx, []string{"down"})
	},
}

var restartCmd = &cli.Command{
	Name:  "restart",
	Usage: "run docker compose restart",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "dry-run",
		},
	},
	Before: beforeConfig,
	Action: func(ctx context.Context, cmd *cli.Command) error {
		if cmd.Bool("dry-run") {
			return runCompose(ctx, []string{"restart", "--dry-run"})
		}

		return runCompose(ctx, []string{"restart"})
	},
}

var logsCmd = &cli.Command{
	Name:      "logs",
	Usage:     "run docker compose logs",
	ArgsUsage: "[service]",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "follow",
			Aliases: []string{"f"},
			Usage:   "Follow the logs.",
		},
	},
	Before: beforeConfig,
	Action: func(ctx context.Context, cmd *cli.Command) error {
		args := []string{"logs"}

		if cmd.Bool("follow") {
			args = append(args, "--follow")
		}

		if cmd.Args().Len() > 0 {
			args = append(args, cmd.Args().Slice()...)
		}

		return runCompose(ctx, args)
	},
}

var composeCmd = &cli.Command{
	Name:   "compose",
	Usage:  "Runs docker compose commands.",
	Before: beforeConfig,
	Action: func(ctx context.Context, cmd *cli.Command) error {
		// Capture arguments after "--"
		if idx := slices.Index(cmd.Args().Slice(), "--"); idx != -1 {
			args := cmd.Args().Slice()[idx+1:]
			return runCompose(ctx, args)
		}
		return runCompose(ctx, []string{"compose"})
	},
}

var statusCmd = &cli.Command{
	Name:   "status",
	Usage:  "run docker compose ps -a",
	Before: beforeConfig,
	Action: func(ctx context.Context, cmd *cli.Command) error {
		return runCompose(ctx, []string{"ps", "-a"})
	},
}

var showCmd = &cli.Command{
	Name:   "show",
	Usage:  "run docker compose config",
	Before: beforeConfig,
	Action: func(ctx context.Context, cmd *cli.Command) error {
		return runCompose(ctx, []string{"config"})
	},
}
