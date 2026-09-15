package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSync(t *testing.T) {
	dir := t.TempDir()
	cat := filepath.Join(dir, "catalog.yaml")
	write(t, cat, `
metadata: {id: X}
controls:
  - id: X.C01
    assessment-requirements:
      - {id: X.C01.TR01}
      - {id: X.C01.TR02}
      - {id: X.C01.TR03, state: Retired}
`)
	// Typed map with one bound id, one orphan, plus a non-step map that must be ignored.
	write(t, filepath.Join(dir, "plugin", "evaluation_plans", "plans.go"), `package evaluation_plans
var Suite = map[string][]TypedStep{
	"X.C01.TR01": {Foo},
	"X.C01.TR09": {Bar},
}
var notSteps = map[string][]string{"X.C01.TR02": {"ignored"}}
`)
	// Scaffold shape, untyped, in a second file.
	write(t, filepath.Join(dir, "plugin", "other.go"), `package other
var S = map[string][]gemara.AssessmentStep{"X.C01.TR03": {NotImplemented}}
`)

	r, err := buildSync(cat, filepath.Join(dir, "plugin"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := r.Missing, []string{"X.C01.TR02"}; !reflect.DeepEqual(got, want) {
		t.Errorf("missing = %v, want %v", got, want)
	}
	// TR03 is retired: bound but not live, so it is an orphan alongside TR09.
	if got, want := r.Orphaned, []string{"X.C01.TR03", "X.C01.TR09"}; !reflect.DeepEqual(got, want) {
		t.Errorf("orphaned = %v, want %v", got, want)
	}
	if r.Synced {
		t.Error("expected drift")
	}
}

func TestRunSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.yaml")
	write(t, path, `
service-name: svc
plugin-name: plug
evaluation-suites:
  - name: s1
    catalog-id: X
    result: Failed
    control-evaluations:
      evaluations:
        - control: {entry-id: X.C01}
          assessment-logs:
            - requirement: {entry-id: X.C01.TR01}
              result: Passed
              steps-executed: 2
            - requirement: {entry-id: X.C01.TR02}
              result: Failed
              steps-executed: 1
            - requirement: {}
              result: Unknown
`)
	s, err := summarize(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Totals["Passed"] != 1 || s.Totals["Failed"] != 1 {
		t.Errorf("totals = %v", s.Totals)
	}
	if len(s.UnknownIDs) != 1 {
		t.Errorf("unknown ids = %v", s.UnknownIDs)
	}
	if s.Suites[0].ByAR["X.C01.TR01"].StepsExecuted != 2 {
		t.Errorf("by_ar = %v", s.Suites[0].ByAR)
	}
}
