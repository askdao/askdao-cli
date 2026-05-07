package scanner

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/askdao/askdao-cli/internal/types"
)

// ParseDockerfile reads a Dockerfile at path and returns the v0.4 full AST plus
// extracted apt/pip/setup_commands and Anthropic-compat warnings. If the file
// does not exist, returns (&DetectedDockerfile{Exists:false}, nil) so callers
// can either skip or include the negative-result entry.
func ParseDockerfile(path string) (*types.DetectedDockerfile, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &types.DetectedDockerfile{Exists: false}, nil
	} else if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	res, err := parser.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("dockerfile parse: %w", err)
	}

	df := &types.DetectedDockerfile{
		Exists:                 true,
		Path:                   path,
		EnvVars:                map[string]string{},
		Cmd:                    []string{},
		Entrypoint:             []string{},
		BuildArgs:              []string{},
		ExtractedAptPackages:   []string{},
		ExtractedPipPackages:   []string{},
		ExtractedSetupCommands: []string{},
	}

	var stages []types.DockerStage
	var current *types.DockerStage

	for _, child := range res.AST.Children {
		instr := strings.ToUpper(child.Value)
		args := collectArgs(child)

		switch instr {
		case "FROM":
			from, asName := parseFromArgs(args)
			stage := types.DockerStage{From: from, As: asName, Commands: []types.DockerCommand{}}
			stages = append(stages, stage)
			current = &stages[len(stages)-1]

		default:
			if current == nil {
				// Instruction outside any FROM (parser directives etc.) — ignore.
				continue
			}
			rawValue := joinArgsRaw(args)
			current.Commands = append(current.Commands, types.DockerCommand{
				Instruction: instr,
				Value:       rawValue,
			})
			applyInstruction(df, instr, args, rawValue)
		}
	}

	df.Stages = stages
	if len(stages) > 0 {
		last := stages[len(stages)-1]
		df.BaseImage = last.From
		df.FinalStageName = last.As
	}

	df.AnthropicCompatibleWarnings = buildAnthropicWarnings(df)
	return df, nil
}

// collectArgs walks node.Next chain into a flat slice of token strings.
func collectArgs(node *parser.Node) []string {
	var out []string
	for n := node.Next; n != nil; n = n.Next {
		out = append(out, n.Value)
	}
	return out
}

// joinArgsRaw glues args back into a single value string for storage in the
// AST commands list. Quotes are not re-added — buildkit's parser already strips
// them, and consumers downstream re-tokenize for shell-form RUN parsing.
func joinArgsRaw(args []string) string {
	return strings.Join(args, " ")
}

// parseFromArgs splits "image[:tag] [AS name]" into image + optional alias.
func parseFromArgs(args []string) (string, *string) {
	if len(args) == 0 {
		return "", nil
	}
	image := args[0]
	for i := 1; i+1 < len(args); i++ {
		if strings.EqualFold(args[i], "AS") {
			name := args[i+1]
			return image, &name
		}
	}
	return image, nil
}

func applyInstruction(df *types.DetectedDockerfile, instr string, args []string, raw string) {
	switch instr {
	case "RUN":
		df.RunCommands = append(df.RunCommands, raw)
		extractFromRun(df, raw)
	case "WORKDIR":
		if len(args) > 0 {
			df.Workdir = args[0]
		}
	case "ENV":
		for k, v := range parseEnvArgs(args) {
			df.EnvVars[k] = v
		}
	case "EXPOSE":
		for _, a := range args {
			port := strings.SplitN(a, "/", 2)[0]
			if n, err := strconv.Atoi(port); err == nil {
				df.ExposedPorts = append(df.ExposedPorts, n)
			}
		}
	case "USER":
		if len(args) > 0 {
			df.Users = append(df.Users, types.DockerUser{Name: args[0]})
		}
	case "CMD":
		df.Cmd = args
	case "ENTRYPOINT":
		df.Entrypoint = args
	case "ARG":
		for _, a := range args {
			name := strings.SplitN(a, "=", 2)[0]
			df.BuildArgs = append(df.BuildArgs, name)
		}
	}
}

// parseEnvArgs handles both ENV forms. buildkit's parser normalizes
//
//	ENV KEY=value KEY2=value2
//	ENV KEY value with spaces
//
// into a flat alternating key/value/key/value list with an "=" or "" terminator
// node — see docs/investigations comment in dockerfile_test.go.
func parseEnvArgs(args []string) map[string]string {
	out := map[string]string{}
	for i := 0; i+1 < len(args); i += 2 {
		k := args[i]
		v := args[i+1]
		if k == "" || k == "=" {
			continue
		}
		out[k] = v
	}
	return out
}

// aptInstallRe matches `apt-get install [-y] pkg1 pkg2 ...` or `apt install ...`,
// stopping the package list at the first shell metachar (&, |, ;, redirect).
var aptInstallRe = regexp.MustCompile(`(?:apt-get|apt)\s+(?:-[a-zA-Z]+\s+)*install\s+((?:-[a-zA-Z]+\s+|--[a-zA-Z-]+(?:=\S+)?\s+)*)([^&|;<>]+)`)

// pipInstallRe matches `pip install ...` / `pip3 install ...`. We capture the
// trailing args; -r requirements.txt invocations are ignored.
var pipInstallRe = regexp.MustCompile(`pip3?\s+install\s+([^&|;<>]+)`)

// extractFromRun splits a RUN line on shell `&&` and bins each fragment into
// apt / pip / leftover-setup buckets per design.md §4.
func extractFromRun(df *types.DetectedDockerfile, raw string) {
	for _, frag := range splitShellAnd(raw) {
		frag = strings.TrimSpace(frag)
		if frag == "" {
			continue
		}
		if isHousekeeping(frag) {
			continue
		}
		if matches := aptInstallRe.FindStringSubmatch(frag); matches != nil {
			pkgs := tokenizePackages(matches[2])
			df.ExtractedAptPackages = append(df.ExtractedAptPackages, pkgs...)
			continue
		}
		if matches := pipInstallRe.FindStringSubmatch(frag); matches != nil {
			tokens := tokenizePackages(matches[1])
			for _, t := range tokens {
				if strings.HasPrefix(t, "-") {
					// Skip flag args like `-r requirements.txt`, `--no-cache-dir`.
					continue
				}
				df.ExtractedPipPackages = append(df.ExtractedPipPackages, t)
			}
			continue
		}
		df.ExtractedSetupCommands = append(df.ExtractedSetupCommands, frag)
	}
}

// splitShellAnd is a deliberately naive splitter on `&&` — quoted-string-safe
// shell parsing is overkill for Dockerfile RUN heuristics, and a false positive
// just creates an extra setup_commands entry.
func splitShellAnd(s string) []string {
	return strings.Split(s, "&&")
}

// isHousekeeping filters trivial commands that pollute setup_commands without
// telling adapters anything actionable.
func isHousekeeping(frag string) bool {
	low := strings.ToLower(strings.TrimSpace(frag))
	switch {
	case strings.HasPrefix(low, "apt-get update"),
		strings.HasPrefix(low, "apt update"),
		strings.HasPrefix(low, "rm -rf /var/lib/apt"),
		strings.HasPrefix(low, "rm -rf /var/cache"):
		return true
	}
	return false
}

// tokenizePackages splits on whitespace and drops empties.
func tokenizePackages(s string) []string {
	fields := strings.Fields(s)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f == "" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// buildAnthropicWarnings produces v0.4 per-field compat warnings the recommender
// surfaces in the KOL review card.
func buildAnthropicWarnings(df *types.DetectedDockerfile) []types.DockerCompatWarning {
	var ws []types.DockerCompatWarning
	if len(df.Stages) > 1 {
		ws = append(ws, types.DockerCompatWarning{
			Field: "stages",
			Issue: "multi-stage build not supported; only final stage's packages can be migrated",
		})
	}
	if len(df.Users) > 0 {
		ws = append(ws, types.DockerCompatWarning{
			Field: "users",
			Issue: "USER directive ignored; Anthropic runs as fixed user",
		})
	}
	if len(df.ExposedPorts) > 0 {
		ws = append(ws, types.DockerCompatWarning{
			Field: "exposed_ports",
			Issue: "EXPOSE ignored; no port preview",
		})
	}
	if len(df.Entrypoint) > 0 {
		ws = append(ws, types.DockerCompatWarning{
			Field: "entrypoint",
			Issue: "ENTRYPOINT ignored; Anthropic launches its own runtime",
		})
	}
	return ws
}
