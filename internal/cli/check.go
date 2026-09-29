package cli

import "fmt"

// Check reports whether args (without the binary name) name a real command
// with flags it accepts, without running it. The landing page's clip shows
// command lines from a recorded session; this is how a test notices when
// one of them is no longer something the binary takes.
func Check(args []string) error {
	root := newRootCommand(&env{})
	cmd, rest, err := root.Find(args)
	if err != nil {
		return err
	}
	if cmd == root {
		return fmt.Errorf("%q is not a yakanban command", args)
	}
	if err := cmd.ParseFlags(rest); err != nil {
		return fmt.Errorf("%s: %w", cmd.CommandPath(), err)
	}
	return cmd.ValidateArgs(cmd.Flags().Args())
}
