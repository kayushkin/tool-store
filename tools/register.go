package tools

// Auto-registers the zero-argument tools into the in-process registry. Tools
// whose constructors require caller context (e.g. RepoMap, RecentFiles,
// ScratchpadTool, TaskPlanTool) are NOT auto-registered — consumers that need
// them call the factory with their own context and then Register the result.
func init() {
	Register(Shell())
	Register(Browser())
	Register(EndTurn())
	Register(ReadFile())
	Register(WriteFile())
	Register(EditFile())
	Register(ListFiles())
	Register(Grep())
	Register(WebFetch())
	Register(WebSearch())
	Register(Scheduler())
}
