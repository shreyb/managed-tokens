package main

import (
	"fmt"
	"os"

	"github.com/spf13/pflag"
)

// Custom usage function for positional argument.
func onboardingUsage() {
	fmt.Printf("Usage: %s [OPTIONS] service...\n", os.Args[0])
	fmt.Printf("service must be of the form 'experiment_role', e.g. 'dune_production'\n")
	pflag.PrintDefaults()
}
