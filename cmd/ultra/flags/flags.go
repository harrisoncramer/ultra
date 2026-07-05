package flags

// sharedFlags are the flags every resolver subcommand inherits from run.
type SharedFlags struct {
	Root    string
	AppsDir string
}
