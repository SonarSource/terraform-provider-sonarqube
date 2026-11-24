// Command schemacheck writes a snapshot of the provider schemas, and compares
// two snapshots. scripts/schema-check.sh uses it.
//
//	schemacheck snapshot <file>
//	schemacheck compare <base file> <head file>
//
// compare exits with 1 when it finds a breaking change, and with 2 on an
// error.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/schemacheck"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	var err error
	switch {
	case len(args) == 2 && args[0] == "snapshot":
		err = writeSnapshot(args[1])
	case len(args) == 3 && args[0] == "compare":
		var changes []schemacheck.Change
		changes, err = compare(args[1], args[2])
		if err == nil && len(changes) > 0 {
			fmt.Fprintln(stdout, "These schema changes can break the configuration or the state of a user:")
			for _, c := range changes {
				fmt.Fprintln(stdout, "- "+c.String())
			}
			return 1
		}
		if err == nil {
			fmt.Fprintln(stdout, "No breaking schema change.")
		}
	default:
		err = fmt.Errorf("usage: schemacheck snapshot <file> | schemacheck compare <base file> <head file>")
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return 0
}

func writeSnapshot(file string) error {
	snapshot, err := schemacheck.Take(context.Background(), provider.New("schemacheck")())
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(data, '\n'), 0o600) //nolint:gosec // The path comes from the command line.
}

func compare(baseFile, headFile string) ([]schemacheck.Change, error) {
	base, err := readSnapshot(baseFile)
	if err != nil {
		return nil, err
	}
	head, err := readSnapshot(headFile)
	if err != nil {
		return nil, err
	}
	return schemacheck.Compare(base, head), nil
}

func readSnapshot(file string) (schemacheck.Snapshot, error) {
	data, err := os.ReadFile(file) //nolint:gosec // The path comes from the command line.
	if err != nil {
		return nil, err
	}
	var snapshot schemacheck.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return snapshot, nil
}
