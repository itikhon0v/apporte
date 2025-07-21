package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/BurntSushi/toml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
)

type TomlRule struct {
	Match   string      `toml:"match"`
	Apporte interface{} `toml:"apporte"` // string or []string
}

type TomlConfig struct {
	Rules []TomlRule `toml:"rule"`
}

type Rule struct {
	Match   *regexp.Regexp
	Apporte []string
	Source  string
	Rank    int
	Groups  []string
}

func normalizeApporte(v interface{}) ([]string, error) {
	switch val := v.(type) {
	case string:
		return strings.Fields(val), nil
	case []interface{}:
		var parts []string
		for _, p := range val {
			if s, ok := p.(string); ok {
				parts = append(parts, s)
			} else {
				return nil, fmt.Errorf("non-string in apporte list: %v", p)
			}
		}
		return parts, nil
	default:
		return nil, fmt.Errorf("invalid apporte type: %T", v)
	}
}

func loadRulesFromFile(path string, baseRank int) ([]Rule, error) {
	var tc TomlConfig
	var finalErr error

	if _, err := os.Stat(path); err != nil {
		return nil, nil
	}
	if _, err := toml.DecodeFile(path, &tc); err != nil {
		return nil, fmt.Errorf("failed to parse TOML: %w", err)
	}

	var rules []Rule
	for i, r := range tc.Rules {
		re, err := regexp.Compile(r.Match)
		if err != nil {
			finalErr = errors.Join(finalErr, fmt.Errorf("rule %d: invalid regex %q: %w", i, r.Match, err))
			continue
		}
		apporteStr, err := normalizeApporte(r.Apporte)
		if err != nil {
			finalErr = errors.Join(finalErr, fmt.Errorf("rule %d: invalid apporte: %w", i, err))
			continue
		}
		rules = append(rules, Rule{
			Match:   re,
			Apporte: apporteStr,
			Source:  path,
			Rank:    baseRank + i,
		})
	}

	return rules, finalErr
}

func parentDir(path string) string {
	return filepath.Dir(filepath.Clean(path))
}

func tryLoadRules(
	configPath string,
	rulesCount int,
	visitedPaths map[string]bool,
	allRules *[]Rule,
	finalErr *error,
) int {
	if visitedPaths[configPath] {
		return 0
	}
	visitedPaths[configPath] = true

	rules, err := loadRulesFromFile(configPath, rulesCount)
	if err == nil {
		*allRules = append(*allRules, rules...)
		return len(rules)
	}
	if !os.IsNotExist(err) {
		*finalErr = errors.Join(*finalErr, fmt.Errorf("error in %q: %w", configPath, err))
	}
	return 0
}

func crawlConfigTree(start string, prioritizedConfigPath []string) ([]Rule, error) {
	var allRules []Rule
	var finalErr error
	visitedPaths := map[string]bool{}
	rulesCount := 0

	// prioritized paths (rank 0+)
	for _, configPath := range prioritizedConfigPath {
		rulesCount += tryLoadRules(configPath, rulesCount, visitedPaths, &allRules, &finalErr)
	}

	// $PWD -> root
	dir := start
	for {
		configPath := filepath.Join(dir, ".apporte.toml")
		rulesCount += tryLoadRules(configPath, rulesCount, visitedPaths, &allRules, &finalErr)

		parent := parentDir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// user config is lowest priority
	if userConfDir, err := os.UserConfigDir(); err == nil {
		configPath := filepath.Join(userConfDir, ".apporte.toml")
		rulesCount += tryLoadRules(configPath, rulesCount, visitedPaths, &allRules, &finalErr)
	}

	return allRules, finalErr
}

func matchRule(input string, rule Rule) (Rule, bool) {
	result := rule.Match.FindStringSubmatch(input)
	if result == nil {
		rule.Groups = result
		return Rule{}, false
	}
	rule.Groups = result
	return rule, true
}

func matchRules(input string, rules []Rule) ([]Rule, error) {
	var (
		matched []Rule
		mu      sync.Mutex
		wg      sync.WaitGroup
	)

	concurrency := runtime.NumCPU()
	sem := make(chan struct{}, concurrency)

	for _, rule := range rules {
		wg.Add(1)

		go func(r Rule) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if matchedRule, ok := matchRule(input, r); ok {
				mu.Lock()
				matched = append(matched, matchedRule)
				mu.Unlock()
			}
		}(rule)
	}

	wg.Wait()
	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].Rank < matched[j].Rank
	})

	return matched, nil
}

func expandApporte(rules []Rule) []Rule {
	for i := range rules {
		for j, group := range rules[i].Groups {
			placeholder := fmt.Sprintf("$%d", j)
			for k, part := range rules[i].Apporte {
				rules[i].Apporte[k] = strings.ReplaceAll(part, placeholder, group)
			}
		}
	}
	return rules
}

func dispatch(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("empty command")
	}

	if runtime.GOOS == "windows" {
		// syscall.Exec is a noop on Windows
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	binary, err := exec.LookPath(argv[0])
	if err != nil {
		return fmt.Errorf("command not found: %s", argv[0])
	}
	return syscall.Exec(binary, argv, os.Environ())
}

func main() {
	var (
		longExplain    = flag.Bool("explain", false, "")
		shortExplain   = flag.Bool("e", false, "Show details without dispatching")
		longVerbose    = flag.Bool("verbose", false, "")
		shortVerbose   = flag.Bool("v", false, "Show details and dispatch")
		longConfig     = flag.String("config", "", "")
		shortConfig    = flag.String("c", "", "Prioritized config path")
		inputFlag      = flag.String("input", "", "")
		inputFlagShort = flag.String("i", "", "Input to match against")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage of %s [OPTION] [-i|--input] FILE...
  -c, --config		Prioritized config path
  -e, --explain		Show details without dispatching
  -h, --help		Show this message
  -i, --input		Input to match against
  -v, --verbose		Show details and dispatch
`, os.Args[0])
	}
	flag.Parse()

	explain := *longExplain || *shortExplain
	verbose := *longVerbose || *shortVerbose

	config := *longConfig
	if *shortConfig != "" {
		config = *shortConfig
	}

	var input string

	switch {
	case *inputFlag != "":
		input = *inputFlag
	case *inputFlagShort != "":
		input = *inputFlagShort
	default:
		args := flag.Args()
		if len(args) > 0 {
			input = args[0]
		} else {
			stat, _ := os.Stdin.Stat()
			if (stat.Mode() & os.ModeCharDevice) == 0 {
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to read stdin: %v\n", err)
					os.Exit(1)
				}
				input = strings.TrimSpace(string(data))
			}
		}
	}

	if input == "" {
		fmt.Fprintln(os.Stderr, "No input provided. Use -i, positional arg, or pipe stdin.")
		os.Exit(1)
	}

	startDir, _ := os.Getwd()
	rules, err := crawlConfigTree(startDir, []string{config})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warnings while loading rules:\n%s\n", err)
	}

	matched, err := matchRules(input, rules)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error matching rules: %v\n", err)
		os.Exit(1)
	}
	if len(matched) == 0 {
		fmt.Println("No rules matched.")
		return
	}

	matched = expandApporte(matched)
	selected := matched[0]

	if explain || verbose {
		fmt.Printf("Input		: %s\n", input)
		fmt.Printf("Matched		: %s\n", selected.Match)
		fmt.Printf("From File	: %s\n", selected.Source)
		fmt.Printf("Command		: %v\n", selected.Apporte)
		fmt.Printf("Rank		: %d\n", selected.Rank)
		fmt.Printf("Groups		: %v\n", selected.Groups)
		fmt.Println()
	}

	if explain {
		return
	}

	if err := dispatch(selected.Apporte); err != nil {
		fmt.Fprintf(os.Stderr, "Dispatch failed: %v\n", err)
		os.Exit(1)
	}
}
