package process

const (
	PermissionReadFile  = "read_file"
	PermissionWriteFile = "write_file"
	PermissionRunTests  = "run_tests"
	PermissionGitStatus = "git_status"
	PermissionCommit    = "commit"
)

// PermissionSet captures P0/P1 side-effect boundaries for a stage or action.
type PermissionSet struct {
	ReadFile  bool
	WriteFile bool
	RunTests  bool
	GitStatus bool
	Commit    bool
}

// P0Permissions blocks all LLM-owned side effects. Only answers, plans,
// classifications, findings and transition proposals are allowed.
func P0Permissions() PermissionSet {
	return PermissionSet{}
}
