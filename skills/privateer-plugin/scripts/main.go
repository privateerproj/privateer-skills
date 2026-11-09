// Inspection helpers for the privateer-plugin skill.
//
//	sync          diff catalog requirement ids against the ids a plugin's step map registers
//	run-summary   parse a pvtr EvaluationLog and emit per-requirement results
//
// JSON on stdout, diagnostics on stderr. Exit 0 ok, 1 drift under --strict,
// 2 usage error or unreadable input.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	args := os.Args[2:]
	switch os.Args[1] {
	case "sync":
		os.Exit(cmdSync(args))
	case "run-summary":
		os.Exit(cmdRunSummary(args))
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: go run . <subcommand> [flags]

  sync          --catalog <catalog.yaml or data/catalogs dir> --plugin <plugin-source-dir> [--strict]
  run-summary   --path <evaluation-log.yaml>
`)
}

// ---------------- catalog ----------------

type catalog struct {
	Metadata struct {
		ID string `yaml:"id"`
	} `yaml:"metadata"`
	Controls []struct {
		ID  string `yaml:"id"`
		ARs []struct {
			ID    string `yaml:"id"`
			State string `yaml:"state"`
		} `yaml:"assessment-requirements"`
	} `yaml:"controls"`
}

// liveRequirementIDs returns the ids a plugin must bind (every requirement not
// in state Retired) and the retired ids, which a plugin may keep bound to a
// Retired step. Duplicate ids are reported so a copy-paste in the catalog does
// not hide as a single registration.
func liveRequirementIDs(path string) (ids, retired, dupes []string, err error) {
	ids, retired, dupes = []string{}, []string{}, []string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, err
	}
	var c catalog
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	seen := map[string]bool{}
	for _, ctl := range c.Controls {
		for _, ar := range ctl.ARs {
			if seen[ar.ID] {
				dupes = append(dupes, ar.ID)
				continue
			}
			seen[ar.ID] = true
			if ar.State == "Retired" {
				retired = append(retired, ar.ID)
			} else {
				ids = append(ids, ar.ID)
			}
		}
	}
	sort.Strings(ids)
	sort.Strings(retired)
	sort.Strings(dupes)
	return ids, retired, dupes, nil
}

// ---------------- plugin ----------------

type registration struct {
	ID   string `json:"id"`
	File string `json:"file"`
	Line int    `json:"line"`
}

// registeredIDs walks Go source under dir and collects the string-literal keys
// of every composite literal typed map[string][]<...Step>. That is the shape
// both the scaffold (gemara.AssessmentStep) and typed plugins (TypedStep) use.
// A non-literal key cannot be resolved statically and is reported as dynamic.
func registeredIDs(dir string) (ids []string, regs []registration, dynamic []registration, err error) {
	ids, regs, dynamic = []string{}, []registration{}, []registration{}
	seen := map[string]bool{}
	fset := token.NewFileSet()
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == "vendor" || (strings.HasPrefix(n, ".") && n != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok || !isStepMap(cl.Type) {
				return true
			}
			for _, elt := range cl.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				pos := fset.Position(kv.Pos())
				lit, ok := kv.Key.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					dynamic = append(dynamic, registration{"", path, pos.Line})
					continue
				}
				id := strings.Trim(lit.Value, "`\"")
				regs = append(regs, registration{id, path, pos.Line})
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
			return true
		})
		return nil
	})
	sort.Strings(ids)
	return ids, regs, dynamic, err
}

// isStepMap matches map[string][]X where X's identifier ends in "Step".
func isStepMap(expr ast.Expr) bool {
	mt, ok := expr.(*ast.MapType)
	if !ok {
		return false
	}
	if k, ok := mt.Key.(*ast.Ident); !ok || k.Name != "string" {
		return false
	}
	at, ok := mt.Value.(*ast.ArrayType)
	if !ok {
		return false
	}
	switch e := at.Elt.(type) {
	case *ast.SelectorExpr:
		return strings.HasSuffix(e.Sel.Name, "Step")
	case *ast.Ident:
		return strings.HasSuffix(e.Name, "Step")
	}
	return false
}

// ---------------- sync ----------------

type syncReport struct {
	Catalog       string         `json:"catalog"`
	Plugin        string         `json:"plugin"`
	Missing       []string       `json:"missing_in_plugin"`  // live requirement with no chain
	Orphaned      []string       `json:"orphaned_in_plugin"` // chain whose id is not in the catalog
	DuplicateIDs  []string       `json:"duplicate_ids_in_catalog"`
	DynamicKeys   []registration `json:"dynamic_keys"` // map keys that are not string literals
	Registrations []registration `json:"registrations"`
	Synced        bool           `json:"synced"`
}

func cmdSync(args []string) int {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	catPath := fs.String("catalog", "", "catalog YAML, or a directory of them (the plugin's data/catalogs)")
	pluginDir := fs.String("plugin", "", "plugin source directory")
	strict := fs.Bool("strict", false, "exit 1 on drift")
	_ = fs.Parse(args)
	if *catPath == "" || *pluginDir == "" {
		fmt.Fprintln(os.Stderr, "sync: --catalog and --plugin required")
		return 2
	}
	r, err := buildSync(*catPath, *pluginDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sync: %v\n", err)
		return 2
	}
	emitJSON(r)
	if !r.Synced && *strict {
		return 1
	}
	return 0
}

// catalogLiveIDs accepts one catalog file, or a directory such as a plugin's
// data/catalogs, whose catalogs are checked together: a multi-catalog plugin
// registers every catalog's ids, so checking one alone reports the rest as
// orphans. Duplicates count within a file only; the same id in two catalog
// versions is expected.
func catalogLiveIDs(catPath string) (live, retired, dupes []string, err error) {
	files := []string{catPath}
	if fi, err := os.Stat(catPath); err == nil && fi.IsDir() {
		yml, _ := filepath.Glob(filepath.Join(catPath, "*.yml"))
		files, _ = filepath.Glob(filepath.Join(catPath, "*.yaml"))
		files = append(files, yml...)
		if len(files) == 0 {
			return nil, nil, nil, fmt.Errorf("no *.yaml or *.yml catalogs in %s", catPath)
		}
	}
	seen := map[string]bool{}
	live, retired, dupes = []string{}, []string{}, []string{}
	for _, f := range files {
		ids, ret, d, err := liveRequirementIDs(f)
		if err != nil {
			return nil, nil, nil, err
		}
		dupes = append(dupes, d...)
		retired = append(retired, ret...)
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				live = append(live, id)
			}
		}
	}
	sort.Strings(live)
	sort.Strings(retired)
	sort.Strings(dupes)
	return live, retired, dupes, nil
}

func buildSync(catPath, pluginDir string) (*syncReport, error) {
	live, retired, dupes, err := catalogLiveIDs(catPath)
	if err != nil {
		return nil, err
	}
	reg, regs, dynamic, err := registeredIDs(pluginDir)
	if err != nil {
		return nil, err
	}
	r := &syncReport{
		Catalog:       catPath,
		Plugin:        pluginDir,
		Missing:       diff(live, reg),
		Orphaned:      diff(reg, append(append([]string{}, live...), retired...)), // a bound retired id is kept on purpose
		DuplicateIDs:  dupes,
		DynamicKeys:   dynamic,
		Registrations: regs,
	}
	r.Synced = len(r.Missing) == 0 && len(r.Orphaned) == 0 && len(dupes) == 0 && len(dynamic) == 0
	return r, nil
}

func diff(a, b []string) []string {
	bset := map[string]bool{}
	for _, x := range b {
		bset[x] = true
	}
	out := []string{}
	for _, x := range a {
		if !bset[x] {
			out = append(out, x)
		}
	}
	return out
}

// ---------------- run-summary ----------------

type evalLog struct {
	ServiceName string `yaml:"service-name"`
	PluginName  string `yaml:"plugin-name"`
	Suites      []struct {
		Name      string `yaml:"name"`
		CatalogID string `yaml:"catalog-id"`
		Result    string `yaml:"result"`
		Controls  struct {
			Evaluations []struct {
				Control struct {
					EntryID string `yaml:"entry-id"`
				} `yaml:"control"`
				AssessmentLogs []struct {
					Requirement struct {
						EntryID string `yaml:"entry-id"`
					} `yaml:"requirement"`
					Result        string `yaml:"result"`
					StepsExecuted int    `yaml:"steps-executed"`
					Message       string `yaml:"message"`
				} `yaml:"assessment-logs"`
			} `yaml:"evaluations"`
		} `yaml:"control-evaluations"`
	} `yaml:"evaluation-suites"`
}

type runSummary struct {
	Service    string         `json:"service"`
	Plugin     string         `json:"plugin"`
	Totals     map[string]int `json:"totals"` // result -> count
	Suites     []suiteSummary `json:"suites"`
	UnknownIDs []string       `json:"unknown_ids"` // assessment logs without a requirement id
}

type suiteSummary struct {
	Name      string               `json:"name"`
	CatalogID string               `json:"catalog_id"`
	Result    string               `json:"result"`
	ByAR      map[string]arSummary `json:"by_ar"`
}

type arSummary struct {
	Result        string `json:"result"`
	StepsExecuted int    `json:"steps_executed"`
	Message       string `json:"message"`
}

func cmdRunSummary(args []string) int {
	fs := flag.NewFlagSet("run-summary", flag.ExitOnError)
	path := fs.String("path", "", "path to an EvaluationLog YAML")
	_ = fs.Parse(args)
	if *path == "" {
		fmt.Fprintln(os.Stderr, "run-summary: --path required")
		return 2
	}
	s, err := summarize(*path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run-summary: %v\n", err)
		return 2
	}
	emitJSON(s)
	return 0
}

func summarize(path string) (*runSummary, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var log evalLog
	if err := yaml.Unmarshal(b, &log); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	s := &runSummary{Service: log.ServiceName, Plugin: log.PluginName, Totals: map[string]int{}, UnknownIDs: []string{}}
	for _, suite := range log.Suites {
		ss := suiteSummary{Name: suite.Name, CatalogID: suite.CatalogID, Result: suite.Result, ByAR: map[string]arSummary{}}
		for _, ce := range suite.Controls.Evaluations {
			for _, al := range ce.AssessmentLogs {
				id := al.Requirement.EntryID
				if id == "" {
					s.UnknownIDs = append(s.UnknownIDs, fmt.Sprintf("suite=%s control=%s", suite.Name, ce.Control.EntryID))
					continue
				}
				ss.ByAR[id] = arSummary{al.Result, al.StepsExecuted, al.Message}
				s.Totals[al.Result]++
			}
		}
		s.Suites = append(s.Suites, ss)
	}
	return s, nil
}

func emitJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
