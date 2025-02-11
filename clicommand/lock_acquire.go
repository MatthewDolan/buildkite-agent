package clicommand

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/internal/agentapi"
	"github.com/urfave/cli"
)

const lockAcquireHelpDescription = `Usage:

   buildkite-agent lock acquire [key]

Description:
   Acquires the lock for the given key. ′lock acquire′ will wait (potentially
   forever) until it can acquire the lock, if the lock is already held by
   another process. If multiple processes are waiting for the same lock, there
   is no ordering guarantee of which one will be given the lock next.

Examples:

   $ buildkite-agent lock acquire llama
   $ critical_section()
   $ buildkite-agent lock release llama

`

type LockAcquireConfig struct {
	// Common config options
	LockScope   string `cli:"lock-scope"`
	SocketsPath string `cli:"sockets-path" normalize:"filepath"`

	LockWaitTimeout time.Duration `cli:"lock-wait-timeout"`
}

var LockAcquireCommand = cli.Command{
	Name:        "acquire",
	Usage:       "Acquires a lock from the agent leader",
	Description: lockAcquireHelpDescription,
	Flags: append(
		lockCommonFlags,
		cli.DurationFlag{
			Name:   "lock-wait-timeout",
			Usage:  "If specified, sets a maximum duration to wait for a lock before giving up",
			EnvVar: "BUILDKITE_LOCK_WAIT_TIMEOUT",
		},
	),
	Action: lockAcquireAction,
}

func lockAcquireAction(c *cli.Context) error {
	if c.NArg() != 1 {
		fmt.Fprint(c.App.ErrWriter, lockAcquireHelpDescription)
		os.Exit(1)
	}
	key := c.Args()[0]

	// Load the configuration
	cfg := LockAcquireConfig{}
	loader := cliconfig.Loader{
		CLI:                    c,
		Config:                 &cfg,
		DefaultConfigFilePaths: DefaultConfigFilePaths(),
	}
	warnings, err := loader.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %s\n", err)
		os.Exit(1)
	}
	for _, warning := range warnings {
		fmt.Fprintln(c.App.ErrWriter, warning)
	}

	if cfg.LockScope != "machine" {
		fmt.Fprintln(c.App.Writer, "Only 'machine' scope for locks is supported in this version.")
		os.Exit(1)
	}

	ctx := context.Background()
	if cfg.LockWaitTimeout <= 0 {
		cctx, canc := context.WithTimeout(ctx, cfg.LockWaitTimeout)
		defer canc()
		ctx = cctx
	}

	cli, err := agentapi.NewClient(ctx, agentapi.LeaderPath(cfg.SocketsPath))
	if err != nil {
		fmt.Fprintf(c.App.ErrWriter, lockClientErrMessage, err)
		os.Exit(1)
	}

	for {
		_, done, err := cli.LockCompareAndSwap(ctx, key, "", "acquired")
		if err != nil {
			fmt.Fprintf(c.App.ErrWriter, "Error performing compare-and-swap: %v\n", err)
			os.Exit(1)
		}

		if done {
			return nil
		}

		// Not done.
		if err := sleep(ctx, 100*time.Millisecond); err != nil {
			fmt.Fprintf(c.App.ErrWriter, "Exceeded deadline or context cancelled: %v\n", err)
			os.Exit(1)
		}
	}
}
