# privateer-skills

[Agent Skills](https://agentskills.io) for working with
[Privateer](https://privateerproj.com). Each skill is a directory under
`skills/` with a `SKILL.md` and its supporting references, and works with any
agent that reads the Agent Skills format (Claude Code, Codex, Cursor, and
others).

| Skill | Use it to |
| --- | --- |
| [`privateer-plugin`](skills/privateer-plugin/SKILL.md) | Build or extend a Privateer plugin idiomatically against the current SDK: scaffold, loader, typed steps, tests, sync check, run. |

## Install

### Claude Code plugin

Add this repository as a plugin marketplace, then install the `privateer`
plugin:

```text
/plugin marketplace add privateerproj/privateer-skills
/plugin install privateer@privateer
```

The skill then runs as `/privateer:privateer-plugin`. Each install stays on
its release until `version` in `.claude-plugin/plugin.json` changes, so commits
between releases never reach you. Claude Code doesn't auto-update third-party
marketplaces by default. Turn on auto-update for `privateer` under `/plugin`,
or run `claude plugin update privateer@privateer`.

### Any agent

Copy or symlink a skill directory into wherever your agent loads skills from.
For Claude Code without the plugin, that is `.claude/skills/` in a project or
`~/.claude/skills/` for every project:

```sh
git clone https://github.com/privateerproj/privateer-skills
ln -s "$PWD/privateer-skills/skills/privateer-plugin" ~/.claude/skills/privateer-plugin
```

The `privateer-plugin` skill shells out to a small Go program in its `scripts/`
directory; it needs a Go toolchain and `pvtr` on the path.

## Keeping skills current

Skills describe conventions, not code. The sources of truth are the
`pvtr generate-plugin` output, the `privateer-sdk` godoc, and the reference
plugin `pvtr-github-repo-scanner`. When a skill and the SDK disagree, the SDK
is right and the skill needs a patch; each skill says which SDK version it was
verified against.

## License

Apache-2.0.
