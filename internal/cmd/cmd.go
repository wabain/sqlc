package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/trace"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	yaml "gopkg.in/yaml.v3"

	"github.com/kyleconroy/sqlc/internal/config"
	"github.com/kyleconroy/sqlc/internal/debug"
	"github.com/kyleconroy/sqlc/internal/tracer"
)

// Do runs the command logic.
func Do(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	rootCmd := &cobra.Command{Use: "sqlc", SilenceUsage: true}
	rootCmd.PersistentFlags().StringP("file", "f", "", "specify an alternate config file (default: sqlc.yaml)")
	rootCmd.PersistentFlags().BoolP("experimental", "x", false, "enable experimental features (default: false)")

	rootCmd.AddCommand(checkCmd)
	rootCmd.AddCommand(genCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(versionCmd)

	rootCmd.SetArgs(args)
	rootCmd.SetIn(stdin)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)

	ctx, cleanup, err := tracer.Start(context.Background())
	if err != nil {
		fmt.Printf("failed to start trace: %v\n", err)
		return 1
	}
	defer cleanup()

	if err := rootCmd.ExecuteContext(ctx); err == nil {
		return 0
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		return exitError.ExitCode()
	}
	return 1
}

var version string

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the sqlc version number",
	Run: func(cmd *cobra.Command, args []string) {
		if debug.Traced {
			defer trace.StartRegion(cmd.Context(), "version").End()
		}
		if version == "" {
			// When no version is set, return the next bug fix version
			// after the most recent tag
			fmt.Printf("%s\n", "v1.10.0")
		} else {
			fmt.Printf("%s\n", version)
		}
	},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create an empty sqlc.yaml settings file",
	RunE: func(cmd *cobra.Command, args []string) error {
		if debug.Traced {
			defer trace.StartRegion(cmd.Context(), "init").End()
		}
		file := "sqlc.yaml"
		if f := cmd.Flag("file"); f != nil && f.Changed {
			file = f.Value.String()
			if file == "" {
				return fmt.Errorf("file argument is empty")
			}
		}
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			return nil
		}
		blob, err := yaml.Marshal(config.V1GenerateSettings{Version: "1"})
		if err != nil {
			return err
		}
		return os.WriteFile(file, blob, 0644)
	},
}

type Env struct {
	ExperimentalFeatures bool
}

func ParseEnv(c *cobra.Command) Env {
	x := c.Flag("experimental")
	return Env{ExperimentalFeatures: x != nil && x.Changed}
}

func getConfigPath(stderr io.Writer, f *pflag.Flag) (string, string) {
	if f != nil && f.Changed {
		file := f.Value.String()
		if file == "" {
			fmt.Fprintln(stderr, "error parsing config: file argument is empty")
			os.Exit(1)
		}
		abspath, err := filepath.Abs(file)
		if err != nil {
			fmt.Fprintf(stderr, "error parsing config: absolute file path lookup failed: %s\n", err)
			os.Exit(1)
		}
		return filepath.Dir(abspath), filepath.Base(abspath)
	} else {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(stderr, "error parsing sqlc.json: file does not exist")
			os.Exit(1)
		}
		return wd, ""
	}
}

var genCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate Go code from SQL",
	Run: func(cmd *cobra.Command, args []string) {
		if debug.Traced {
			defer trace.StartRegion(cmd.Context(), "generate").End()
		}
		stderr := cmd.ErrOrStderr()
		dir, name := getConfigPath(stderr, cmd.Flag("file"))
		output, err := Generate(cmd.Context(), ParseEnv(cmd), dir, name, stderr)
		if err != nil {
			os.Exit(1)
		}
		if debug.Traced {
			defer trace.StartRegion(cmd.Context(), "writefiles").End()
		}
		for filename, source := range output {
			os.MkdirAll(filepath.Dir(filename), 0755)
			if err := os.WriteFile(filename, []byte(source), 0644); err != nil {
				fmt.Fprintf(stderr, "%s: %s\n", filename, err)
				os.Exit(1)
			}
		}
	},
}

var checkCmd = &cobra.Command{
	Use:   "compile",
	Short: "Statically check SQL for syntax and type errors",
	RunE: func(cmd *cobra.Command, args []string) error {
		if debug.Traced {
			defer trace.StartRegion(cmd.Context(), "compile").End()
		}
		stderr := cmd.ErrOrStderr()
		dir, name := getConfigPath(stderr, cmd.Flag("file"))
		if _, err := Generate(cmd.Context(), ParseEnv(cmd), dir, name, stderr); err != nil {
			os.Exit(1)
		}
		return nil
	},
}
