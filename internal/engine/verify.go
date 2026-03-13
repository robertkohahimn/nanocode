package engine

import (
	"fmt"
	"strconv"
	"strings"
)

// VerifyState tracks whether changes have been verified since last edit.
type VerifyState struct {
	pendingVerification bool
	editedFiles         []string
}

// MarkEdit records that a file was edited and verification is now pending.
func (v *VerifyState) MarkEdit(filePath string) {
	v.pendingVerification = true
	// Deduplicate
	for _, f := range v.editedFiles {
		if f == filePath {
			return
		}
	}
	v.editedFiles = append(v.editedFiles, filePath)
}

// MarkVerified records that verification was run.
func (v *VerifyState) MarkVerified() {
	v.pendingVerification = false
	v.editedFiles = nil
}

// IsPending returns true if verification is pending.
func (v *VerifyState) IsPending() bool {
	return v.pendingVerification
}

// EditedFiles returns the list of files edited since last verification.
func (v *VerifyState) EditedFiles() []string {
	return v.editedFiles
}

// ReminderText returns the verification reminder message text.
func (v *VerifyState) ReminderText() string {
	lines := make([]string, 0, len(v.editedFiles))
	for _, file := range v.editedFiles {
		lines = append(lines, "- "+strconv.Quote(file))
	}

	return fmt.Sprintf(
		"<verification-reminder>\nYou made changes to %d file(s) but haven't verified them:\n%s\n\nRun tests or build to verify your changes work before completing.\n</verification-reminder>",
		len(v.editedFiles),
		strings.Join(lines, "\n"),
	)
}

// IsVerifyCommand checks if a bash command is a verification command.
func IsVerifyCommand(cmd string) bool {
	verifyPatterns := []string{
		"go test", "go build", "go vet",
		"npm test", "npm run test", "npm run build",
		"yarn test", "yarn build",
		"pytest", "python -m pytest", "python -m unittest",
		"cargo test", "cargo build", "cargo check",
		"make test", "make build", "make check",
		"mvn test", "mvn compile", "gradle test", "gradle build",
		"dotnet test", "dotnet build",
		"mix test",
	}
	lower := strings.ToLower(strings.TrimSpace(cmd))
	for _, p := range verifyPatterns {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}
