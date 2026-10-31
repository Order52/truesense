# SPDX-License-Identifier: MIT
"""Shell completion scripts generated from the argparse tree (so they never go stale)."""
import argparse


def tree(parser):
    out = {"opts": [o for a in parser._actions for o in a.option_strings if o.startswith("--")], "sub": {}, "choices": []}
    for a in parser._actions:
        if isinstance(a, argparse._SubParsersAction): out["sub"] = {n: tree(p) for n, p in a.choices.items()}
        elif a.choices and not a.option_strings: out["choices"] += [str(c) for c in a.choices]
    return out


def generate(parser, shell):
    t = tree(parser)
    if shell == "fish":
        lines = [f"complete -c truesense -f"]
        for area, sub in t["sub"].items():
            lines.append(f"complete -c truesense -n '__fish_use_subcommand' -a {area}")
            for verb in list(sub["sub"]) + sub["choices"]:
                lines.append(f"complete -c truesense -n '__fish_seen_subcommand_from {area}' -a '{verb}'")
        return "\n".join(lines)
    cases = []
    for area, sub in t["sub"].items():
        words = " ".join(list(sub["sub"]) + sub["choices"] + sub["opts"])
        deep = []
        for verb, s2 in sub["sub"].items():
            w2 = " ".join(s2["choices"] + s2["opts"])
            if w2: deep.append(f'        {verb}) COMPREPLY=( $(compgen -W "{w2}" -- "$cur") ); return;;')
        deep_txt = ("      case ${COMP_WORDS[2]} in\n" + "\n".join(deep) + "\n      esac\n") if deep and False else ""
        cases.append(f'    {area}) if [ "$COMP_CWORD" -eq 2 ]; then COMPREPLY=( $(compgen -W "{words}" -- "$cur") ); else '
                     f'case "${{COMP_WORDS[2]}}" in\n' + "\n".join(deep) + f'\n        *) COMPREPLY=( $(compgen -W "--json" -- "$cur") );; esac; fi;;')
    body = f'''_truesense() {{
  local cur="${{COMP_WORDS[COMP_CWORD]}}"
  if [ "$COMP_CWORD" -eq 1 ]; then COMPREPLY=( $(compgen -W "{' '.join(t['sub'])} --json --help --version" -- "$cur") ); return; fi
  case "${{COMP_WORDS[1]}}" in
{chr(10).join(cases)}
  esac
}}
complete -F _truesense truesense'''
    return body if shell == "bash" else "autoload -U +X bashcompinit && bashcompinit\n" + body
