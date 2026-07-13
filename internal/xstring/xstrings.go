package xstring

import "strings"

func Coalesce(strings ...string) string {
	result := ""
	for _, val := range strings {
		if val != "" {
			result = val
		}
	}
	return result
}

// SplitBy parses a comma-separated required tag into a list of strings.
func SplitBy(s string, delimeter string) []string {
	raw := strings.Split(s, delimeter)
	envs := make([]string, 0, len(raw))
	for _, p := range raw {
		if p = strings.TrimSpace(p); p != "" {
			envs = append(envs, p)
		}
	}
	if len(envs) == 0 {
		return nil
	}
	return envs
}

// hasOption reports whether the comma-separated list contains a value
func CommaSeparatedHasValue(opts, want string) bool {
	for o := range strings.SplitSeq(opts, ",") {
		if strings.TrimSpace(o) == want {
			return true
		}
	}
	return false
}
