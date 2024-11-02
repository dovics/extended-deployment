package version

import "fmt"

var (
	AppVersion  = "v0.0.0"
	GitCommitID = ""
	BuildTime   = "0000-00-00 00:00:00"
	GoVersion   = ""
)

func Version() string {
	return fmt.Sprintf("\n"+
		"AppVersion: %s\n"+
		"  CommitID: %s\n"+
		" BuildTime: %s\n"+
		" GoVersion: %s",
		AppVersion, GitCommitID, BuildTime, GoVersion)
}
