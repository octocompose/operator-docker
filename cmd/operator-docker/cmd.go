package main

import (
	"context"
	"slices"

	"github.com/urfave/cli/v3"

	"github.com/octocompose/operator-docker/pkg/operatorbase"
)

var startCmd = &cli.Command{
	Name:  "start",
	Usage: "run docker compose up -d",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "dry-run",
		},
	},
	Before: operatorbase.BeforeConfig([]string{"docker", "compose"}),
	Action: func(ctx context.Context, cmd *cli.Command) error {
		if cmd.Bool("dry-run") {
			return operatorbase.RunCompose(ctx, []string{"up", "-d", "--dry-run"})
		}

		return operatorbase.RunCompose(ctx, []string{"up", "-d"})
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
	Before: operatorbase.BeforeConfig([]string{"docker", "compose"}),
	Action: func(ctx context.Context, cmd *cli.Command) error {
		if cmd.Bool("dry-run") {
			return operatorbase.RunCompose(ctx, []string{"down", "--dry-run"})
		}

		return operatorbase.RunCompose(ctx, []string{"down"})
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
	Before: operatorbase.BeforeConfig([]string{"docker", "compose"}),
	Action: func(ctx context.Context, cmd *cli.Command) error {
		if cmd.Bool("dry-run") {
			return operatorbase.RunCompose(ctx, []string{"restart", "--dry-run"})
		}

		return operatorbase.RunCompose(ctx, []string{"restart"})
	},
}

var execCmd = &cli.Command{
	Name:      "exec",
	Usage:     "run docker compose exec",
	ArgsUsage: "[service] [command]",
	Before:    operatorbase.BeforeConfig([]string{"docker", "compose"}),
	Action: func(ctx context.Context, cmd *cli.Command) error {
		args := []string{"exec"}

		if cmd.Args().Len() > 0 {
			args = append(args, cmd.Args().Slice()...)
		}

		return operatorbase.RunCompose(ctx, args)
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
	Before: operatorbase.BeforeConfig([]string{"docker", "compose"}),
	Action: func(ctx context.Context, cmd *cli.Command) error {
		args := []string{"logs"}

		if cmd.Bool("follow") {
			args = append(args, "--follow")
		}

		if cmd.Args().Len() > 0 {
			args = append(args, cmd.Args().Slice()...)
		}

		return operatorbase.RunCompose(ctx, args)
	},
}

var composeCmd = &cli.Command{
	Name:   "compose",
	Usage:  "Runs docker compose commands.",
	Before: operatorbase.BeforeConfig([]string{"docker", "compose"}),
	Action: func(ctx context.Context, cmd *cli.Command) error {
		// Capture arguments after "--"
		if idx := slices.Index(cmd.Args().Slice(), "--"); idx != -1 {
			args := cmd.Args().Slice()[idx+1:]
			return operatorbase.RunCompose(ctx, args)
		}
		return operatorbase.RunCompose(ctx, []string{})
	},
}

var statusCmd = &cli.Command{
	Name:   "status",
	Usage:  "run docker compose ps -a",
	Before: operatorbase.BeforeConfig([]string{"docker", "compose"}),
	Action: func(ctx context.Context, cmd *cli.Command) error {
		return operatorbase.RunCompose(ctx, []string{"ps", "-a"})
	},
}

var showCmd = &cli.Command{
	Name:   "show",
	Usage:  "run docker compose config",
	Before: operatorbase.BeforeConfig([]string{"docker", "compose"}),
	Action: func(ctx context.Context, cmd *cli.Command) error {
		return operatorbase.RunCompose(ctx, []string{"config"})
	},
}
