<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

This project is managed by Trellis. The working knowledge you need lives under `.trellis/`:

- `.trellis/workflow.md` — development phases, when to create tasks, skill routing
- `.trellis/spec/` — package- and layer-scoped coding guidelines (read before writing code in a given layer)
- `.trellis/workspace/` — per-developer journals and session traces
- `.trellis/tasks/` — active and archived tasks (PRDs, research, jsonl context)

If a Trellis command is available on your platform (e.g. `/trellis:finish-work`, `/trellis:continue`), prefer it over manual steps. Not every platform exposes every command.

If you're using Codex or another agent-capable tool, additional project-scoped helpers may live in:
- `.agents/skills/` — reusable Trellis skills
- `.codex/agents/` — optional custom subagents

Managed by Trellis. Edits outside this block are preserved; edits inside may be overwritten by a future `trellis update`.

<!-- TRELLIS:END -->

## 补充逻辑
- 工作过程中定期查看我的实时反馈
- codex在等待原生子代理只调用 agents.wait_agent，不得使用 functions.wait 或构造 cell_id
- 日常不需要扫描docs/cankao这个目录下的代码,这里面是参考的开源项目。如果涉及到用户要求参考对应项目的逻辑的时候,优先看这个文件下的项目代码
- 扫描代码的codeGraph使用参考 @CODEGRAPH.md
