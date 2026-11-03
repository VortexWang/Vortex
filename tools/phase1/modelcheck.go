package modelcheck

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"vortex/model"
)

type Report struct {
	Status               string         `json:"status"`
	CommitSHA            string         `json:"commit_sha"`
	ModelSHA256          string         `json:"model_sha256"`
	GoToolchain          string         `json:"go_toolchain"`
	Bounds               map[string]int `json:"bounds"`
	VisitedStates        int            `json:"visited_states"`
	Invariants           []string       `json:"invariants"`
	Violations           []string       `json:"violations"`
	CounterexamplePath   string         `json:"counterexample_path,omitempty"`
	ImplementationStatus string         `json:"implementation_status"`
}

func gitCommit() (string, error) {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return string(out[:len(out)-1]), nil
}

func Run(modelPath, resultDir string) (Report, error) {
	data, err := os.ReadFile(modelPath)
	if err != nil {
		return Report{}, err
	}
	commit, err := gitCommit()
	if err != nil {
		return Report{}, fmt.Errorf("commit metadata: %w", err)
	}
	r := model.Explore()
	violations := append([]string{}, r.Violations...)
	report := Report{Status: "passed", CommitSHA: commit, GoToolchain: runtime.Version(), Bounds: r.Bounds, VisitedStates: r.Visited, Invariants: r.Invariants, Violations: r.Violations, ImplementationStatus: "not-yet-implemented"}
	report.Violations = violations
	h := sha256.Sum256(data)
	report.ModelSHA256 = hex.EncodeToString(h[:])
	if len(r.Violations) > 0 {
		report.Status = "failed"
	}
	if report.CommitSHA == "" || report.ModelSHA256 == "" || report.GoToolchain == "" {
		return Report{}, errors.New("missing model integrity metadata")
	}
	if err := os.MkdirAll(resultDir, 0o755); err != nil {
		return Report{}, err
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return Report{}, err
	}
	if err := os.WriteFile(filepath.Join(resultDir, "model-report.json"), append(encoded, '\n'), 0o644); err != nil {
		return Report{}, err
	}
	return report, nil
}
