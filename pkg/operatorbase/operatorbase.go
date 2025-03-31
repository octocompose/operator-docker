// Package operatorbase contains the base functionality for operators.
package operatorbase

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
	"github.com/octocompose/octoctl/pkg/octoconfig"
	"github.com/urfave/cli/v3"
)

// Context keys
type ComposeFilePathKey struct{}
type ComposeCommandKey struct{}
type LoggerKey struct{}

// ReadConfig reads the config from stdin
func ReadConfig(logger log.Logger, cmd *cli.Command) (map[string]any, error) {
	configFile := cmd.String("config")
	fp, err := os.Open(configFile)
	if err != nil {
		logger.Error("Error while opening config file", "error", err)
		return nil, fmt.Errorf("while opening config file: %w", err)
	}
	defer func() {
		if err := fp.Close(); err != nil {
			logger.Error("Error while closing config file", "error", err)
		}
	}()

	b, err := io.ReadAll(fp)
	if err != nil {
		logger.Error("Error while reading config file", "error", err)
		return nil, fmt.Errorf("while reading config file: %w", err)
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

// PrepareConfig prepares the config
func PrepareConfig(logger log.Logger, data map[string]any) (map[string]any, error) {
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

// WriteConfig writes the config to a file
func WriteConfig(logger log.Logger, data map[string]any, projectID string) (string, error) {
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

// BeforeConfig is a function that is called before the command is executed.
func BeforeConfig(composeCommand []string) func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	return func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
		logger, err := log.New(log.WithLevel(cmd.String("log-level")))
		if err != nil {
			return ctx, err
		}

		ctx = context.WithValue(ctx, LoggerKey{}, logger)

		configData, err := ReadConfig(logger, cmd)
		if err != nil {
			logger.Error("Error while reading config", "error", err)
			os.Exit(1)
		}

		projectID := configData["name"].(string)

		configData, err = PrepareConfig(logger, configData)
		if err != nil {
			logger.Error("Error while reading and preparing config", "error", err)
			os.Exit(1)
		}

		composeFilePath, err := WriteConfig(logger, configData, projectID)
		if err != nil {
			logger.Error("Error while writing config", "error", err)
			os.Exit(1)
		}

		ctx = context.WithValue(ctx, ComposeFilePathKey{}, composeFilePath)
		ctx = context.WithValue(ctx, ComposeCommandKey{}, composeCommand)

		return ctx, nil
	}
}

// RunCmd is a function that is called to run a command.
func RunCmd(ctx context.Context, args []string) error {
	logger := ctx.Value(LoggerKey{}).(log.Logger)
	logger.Debug("Running", "command", args[0], "args", args[1:])

	execCmd := exec.Command(args[0], args[1:]...)
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	if err := execCmd.Run(); err != nil {
		os.Exit(execCmd.ProcessState.ExitCode())
	}

	return nil
}

// RunCompose is a function that is called to run a docker compose command.
func RunCompose(ctx context.Context, args []string) error {
	composeFilePath := ctx.Value(ComposeFilePathKey{}).(string)
	composeCommand := ctx.Value(ComposeCommandKey{}).([]string)

	args2 := append(composeCommand, []string{"-f", composeFilePath}...)
	args2 = append(args2, args...)

	return RunCmd(ctx, args2)
}
