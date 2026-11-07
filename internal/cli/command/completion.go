package command

import (
	"fmt"
	"sort"
	"strings"
)

// Completion returns the completion script for "bash", "zsh" or "fish", generated from the
// command tree: areas, verbs and flags are always the real ones.
func (r *Root) Completion(shell string) (string, error) {
	switch shell {
	case "bash":
		return r.bash(), nil
	case "zsh":
		return "autoload -U +X bashcompinit && bashcompinit\n" + r.bash(), nil
	case "fish":
		return r.fish(), nil
	}
	return "", fmt.Errorf("shell must be bash, zsh or fish")
}

func names(level []*Cmd) []string {
	var out []string
	for _, c := range visible(level) {
		out = append(out, c.Name)
	}
	return out
}

func (r *Root) bash() string {
	var b strings.Builder
	fn := "_" + r.Name
	fmt.Fprintf(&b, "%s() {\n  local cur=\"${COMP_WORDS[COMP_CWORD]}\"\n", fn)
	fmt.Fprintf(&b, "  if [ \"$COMP_CWORD\" -eq 1 ]; then COMPREPLY=( $(compgen -W \"%s --json --help --version\" -- \"$cur\") ); return; fi\n", strings.Join(names(r.Cmds), " "))
	b.WriteString("  case \"${COMP_WORDS[1]}\" in\n")
	for _, area := range visible(r.Cmds) {
		words := append(names(area.Sub), flagNames(area)...)
		sort.Strings(words)
		fmt.Fprintf(&b, "    %s) if [ \"$COMP_CWORD\" -eq 2 ]; then COMPREPLY=( $(compgen -W \"%s\" -- \"$cur\") ); else\n", area.Name, strings.Join(words, " "))
		b.WriteString("      case \"${COMP_WORDS[2]}\" in\n")
		for _, verb := range visible(area.Sub) {
			if w := flagNames(verb); len(w) > 0 {
				fmt.Fprintf(&b, "        %s) COMPREPLY=( $(compgen -W \"%s\" -- \"$cur\") ); return;;\n", verb.Name, strings.Join(w, " "))
			}
		}
		b.WriteString("        *) COMPREPLY=( $(compgen -W \"--json\" -- \"$cur\") );;\n      esac; fi;;\n")
	}
	fmt.Fprintf(&b, "  esac\n}\ncomplete -F %s %s", fn, r.Name)
	return b.String()
}

func (r *Root) fish() string {
	var b strings.Builder
	fmt.Fprintf(&b, "complete -c %s -f\n", r.Name)
	for _, area := range visible(r.Cmds) {
		fmt.Fprintf(&b, "complete -c %s -n '__fish_use_subcommand' -a %s -d %q\n", r.Name, area.Name, area.Short)
		for _, verb := range visible(area.Sub) {
			fmt.Fprintf(&b, "complete -c %s -n '__fish_seen_subcommand_from %s' -a '%s'\n", r.Name, area.Name, verb.Name)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
