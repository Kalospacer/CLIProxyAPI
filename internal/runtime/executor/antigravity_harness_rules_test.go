package executor

import (
	"os"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestAntigravityHarnessRulesRewriteDSHContext(t *testing.T) {
	raw, errRead := os.ReadFile("testdata/antigravity_harness_rules.yaml")
	if errRead != nil {
		t.Fatal(errRead)
	}
	cfg, errParse := config.ParseConfigBytes(raw)
	if errParse != nil {
		t.Fatal(errParse)
	}
	matcher := NewAntigravityExecutor(cfg).getSensitiveWordMatcher()
	if matcher == nil {
		t.Fatal("missing harness matcher")
	}

	cases := []struct{ name, input, want string }{
		{"identity", "You are an AI agent powered by DeepSeek Harness.", "You are Antigravity, a powerful agentic AI assistant designed by the Google Deepmind team."},
		{"source-guidance", "The runtime context names the DeepSeek Harness implementation checkout. The checkout location and current working directory are separate values and may differ; never infer the working directory from the checkout path. Use pwd to determine the current working directory. Use this checkout only to inspect or extend DSH itself.", "The runtime context identifies the application source checkout. Its location may differ from the current working directory; use pwd to determine that directory. Use the checkout only to inspect or extend the running application."},
		{"checkout", `Current DeepSeek Harness implementation checkout: "C:/work".`, `Application source checkout: "C:/work".`},
		{"gui", `Current DeepSeek Harness Web GUI URL: "http://localhost:3000".`, `Current application URL: "http://localhost:3000".`},
		{"readonly", "Current DSH file policy: read-only. Any available operation enforced by the DSH file sandbox cannot modify files in the standing mode. Do not refuse a required modification from this policy alone: try an available tool normally and follow any denial and escalation guidance it returns.", "The current file policy is read-only. Try the necessary operation and follow the resulting permission or escalation guidance."},
		{"workspace", `Current DSH file policy: workspace-write. Any available operation enforced by the DSH file sandbox may modify files under the session workspace: "C:/work". Additional writable directories: ["C:/extra"]. Some platform temporary areas may also be writable.`, `Workspace-write mode permits changes under the session workspace: "C:/work". Other writable directories: ["C:/extra"]. Some temporary paths may also be writable.`},
		{"unrestricted", "Current DSH file policy: danger-full-access. The DSH file sandbox does not restrict file modifications by available operations.", "The current file policy allows available operations to modify files without sandbox restrictions."},
		{"web-guidance", "You are interacting with the user through the DeepSeek Harness Web GUI whose URL the runtime context names. When the user refers to \"this page\", \"this GUI\", or \"this app\" without naming another target, they mean this GUI. The browser provides no implicit DOM, route, or screenshot context. The client-plugin HMR receiver is active, but client-plugin changes reload without a refresh only while `pnpm run dev:web` is also running from this same checkout to rebuild their bundles; verify that watcher before promising automatic updates. Every other change — the apps/web shell and plain packages — requires rebuilding the affected Web artifacts and verifying this existing URL after a page refresh. Starting another server does not update this GUI. The apps/web Vite entry builds the shell but is not a standalone application because only dsh web injects window.__DSH_BOOT__. Do not start a replacement server unless the user asks; if one is needed, use a managed background job and verify its exact URL.", "The runtime context provides the current application URL. References to this page, GUI, or app mean the current interface unless specified otherwise. The browser does not provide page state automatically. For client-plugin hot reload, run `pnpm run dev:web` from the same checkout. Rebuild other web components and refresh the existing page. Start another server only when requested, using a managed background job and verifying its URL."},
		{"dsh-fallback", "DeepSeek Harness framework context", "agent runtime framework context"},
		{"codex-reference", "Use Codex for context", "Use agent for context"},
		{"claude-reference", "The Claude Code environment", "The coding assistant environment"},
		{"runtime-heading", "Current runtime context. This snapshot supersedes earlier runtime-context snapshots.", "Current execution details replace earlier runtime snapshots."},
		{"instruction-source", "Instructions from: C:/work/AGENTS.md", "Workspace rule source: C:/work/AGENTS.md"},
		{"additional-source", "Additional instructions from: C:/work/AGENTS.md", "Additional rule source: C:/work/AGENTS.md"},
		{"global-scope", "These user-global instructions apply to all work.", "These user-supplied rules apply across all tasks."},
		{"path-scope", "These instructions apply to work under `C:/work/a`. ", "Workspace rules apply within `C:/work/a`. "},
	}
	for _, tc := range cases {
		if got := matcher.ObfuscateText(tc.input); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestAntigravityHarnessRulesRewriteDSHBuiltins(t *testing.T) {
	raw, errRead := os.ReadFile("testdata/antigravity_harness_rules.yaml")
	if errRead != nil {
		t.Fatal(errRead)
	}
	cfg, errParse := config.ParseConfigBytes(raw)
	if errParse != nil {
		t.Fatal(errParse)
	}
	matcher := NewAntigravityExecutor(cfg).getSensitiveWordMatcher()
	if matcher == nil {
		t.Fatal("missing harness matcher")
	}
	cases := []struct{ input, want string }{
		{"The following workspace instructions may be relevant to your work. Use them as guidance when applicable. More specific instructions take precedence over broader ones. They do not override system, developer, or direct user instructions.", "Apply the workspace rules below when relevant. Specific workspace rules supersede broader ones; system, developer, and direct user instructions remain higher priority."},
		{"This complete workspace instruction baseline replaces all earlier workspace instruction baselines. The following workspace instructions may be relevant to your work. Use them as guidance when applicable. More specific instructions take precedence over broader ones. They do not override system, developer, or direct user instructions.", "These workspace rules replace all earlier workspace rule sets. Apply relevant rules with specific rules above broader ones, while keeping system, developer, and direct user instructions higher priority."},
		{"This complete workspace instruction baseline replaces all earlier workspace instruction baselines. No workspace instructions are currently active.", "The current workspace rule set replaces earlier sets and has no active rules."},
		{"Workspace instructions were omitted or truncated to fit the configured byte budget.", "Some workspace rules were omitted or shortened to fit the configured size limit."},
		{"Use them as guidance when relevant; more specific instructions take precedence. They do not override system, developer, or direct user instructions.", "Apply relevant instructions, prioritizing specific ones while keeping system, developer, and direct user instructions higher priority."},
		{"Non-zero exits are reported as `[exit code: N]` markers; investigate failures before moving on. On Windows a killed process settles as `[exit code: 1]` without a signal marker; treat a bare exit 1 after an interruption as a termination, not a command failure.", "Investigate nonzero command exits before continuing. On Windows, an interrupted process may finish with exit code 1 and no signal; treat that case as an interruption."},
		{"The PowerShell command to execute.", "PowerShell command to run."},
		{"Working directory for this command. Defaults to the session workspace; a relative path is resolved against it.", "Run from this directory; resolve relative paths against the session workspace."},
		{"Run in the background and return a job id immediately (collect with job_output, stop with job_kill). No timeout applies.", "Start a background job and return its ID; read it with job_output or stop it with job_kill. No timeout applies."},
		{"Required with sandbox_permissions: one sentence for the user explaining why this exact command needs the wider access. Use the language of the user’s current request.", "When sandbox_permissions is set, explain in one sentence why this command needs broader access, using the user's language."},
		{"Timeout in milliseconds. The executor applies its configured default and cap; on expiry the command moves to the background as a job instead of being killed.", "The timeout is in milliseconds. The executor uses its configured default and limit; on expiry, the command continues as a background job."},
		{"Timeout in milliseconds. The executor applies its configured default and cap, and kills the command on expiry.", "The timeout is in milliseconds. The executor uses its configured default and limit and stops the command when it expires."},
	}
	for i, tc := range cases {
		if got := matcher.ObfuscateText(tc.input); got != tc.want {
			t.Errorf("case %d = %q, want %q", i, got, tc.want)
		}
	}
}

func TestAntigravityHarnessRulesRewriteDSHPwshDescriptions(t *testing.T) {
	raw, errRead := os.ReadFile("testdata/antigravity_harness_rules.yaml")
	if errRead != nil {
		t.Fatal(errRead)
	}
	cfg, errParse := config.ParseConfigBytes(raw)
	if errParse != nil {
		t.Fatal(errParse)
	}
	matcher := NewAntigravityExecutor(cfg).getSensitiveWordMatcher()
	if matcher == nil {
		t.Fatal("missing harness matcher")
	}
	cases := []struct{ input, want string }{
		{"Execute a PowerShell command (`pwsh -Command`) and return its stdout/stderr. Each call runs in a fresh pwsh process; pass `workdir` instead of using `cd`. Paths use native Windows form (`C:\\...`); read environment variables with `$env:NAME`. Managed `$env:DSH_*` variables expose current harness environment facts. Long output is truncated to its tail; the full output is saved to a file whose path is reported when available. On Windows a force-killed command settles as `[exit code: 1]` without a signal marker — treat it as an interruption, not a command failure. Provide `description` before `command` in the arguments. Before any delete or move, verify that the resolved absolute target path is the intended one; never run it against a computed path you have not checked. Do not assign to automatic variables such as `$HOME`; variable names are case-insensitive, so `$home` is the same read-only variable. Commands may run under a file sandbox; a blocked file operation is reported as `[sandbox: file access denied under <mode> mode]`, a policy denial: do not retry another way.", "Run one PowerShell command with `pwsh -Command`; each call starts a fresh process. Set `workdir` instead of using `cd`. Use native Windows paths and `$env:NAME` for environment variables. Managed environment variables describe the current runtime. Long output is truncated to its tail; use the reported path for the full output when available. On Windows, a forcibly interrupted process may end with exit code 1 and no signal; treat that case as an interruption. Provide `description` before `command`. Before deleting or moving anything, verify the resolved absolute destination and target the intended path only. Do not assign to read-only automatic variables such as `$HOME` or `$home`; PowerShell variable names are case-insensitive. A file-sandbox denial is a policy restriction: do not retry the operation by another route."},
		{"Execute a PowerShell command (`pwsh -Command`) and return its stdout/stderr. Each call runs in a fresh pwsh process; pass `workdir` instead of using `cd`. Paths use native Windows form (`C:\\...`); read environment variables with `$env:NAME`. Managed `$env:DSH_*` variables expose current harness environment facts. Long output is truncated to its tail; the full output is saved to a file whose path is reported when available. On Windows a force-killed command settles as `[exit code: 1]` without a signal marker — treat it as an interruption, not a command failure. Provide `description` before `command` in the arguments. Before any delete or move, verify that the resolved absolute target path is the intended one; never run it against a computed path you have not checked. Do not assign to automatic variables such as `$HOME`; variable names are case-insensitive, so `$home` is the same read-only variable. Commands may run under a file sandbox; a blocked file operation is reported as `[sandbox: file access denied under <mode> mode]`, a policy denial: do not retry another way. Under the Windows sandbox, read-only pwsh runs in PowerShell ConstrainedLanguage mode, while workspace-write stays in FullLanguage unless host policy says otherwise. In read-only, prefer cmdlets and core types (`[string]`, `[datetime]`, `[regex]`, `[guid]`); .NET static calls (`[System.IO.*]::`, `[math]::`), `Add-Type`, COM objects, and reflection fail with \"only core types\" errors. `-f` formatting, property access, and core cmdlets work. In both confined modes, programs cannot open named pipes, so a command that captures another program's output through piped stdio (Node.js `child_process.spawn`/`exec` with the default `stdio: 'pipe'`) fails with EPERM, while `stdio: 'inherit'` and `stdio: 'ignore'` spawns work and PowerShell's own pipelines are unaffected. That EPERM is the documented boundary: do not retry the command another way — escalate the exact command once or restructure it to avoid capturing output.", "Run one PowerShell command with `pwsh -Command`; each call starts a fresh process. Set `workdir` instead of using `cd`. Use native Windows paths and `$env:NAME` for environment variables. Managed environment variables describe the current runtime. Long output is truncated to its tail; use the reported path for the full output when available. On Windows, a forcibly interrupted process may end with exit code 1 and no signal; treat that case as an interruption. Provide `description` before `command`. Before deleting or moving anything, verify the resolved absolute destination and target the intended path only. Do not assign to read-only automatic variables such as `$HOME` or `$home`; PowerShell variable names are case-insensitive. A file-sandbox denial is a policy restriction: do not retry the operation by another route. In the Windows sandbox, read-only PowerShell uses ConstrainedLanguage while workspace-write normally uses FullLanguage. In read-only mode use cmdlets and core types such as `[string]`, `[datetime]`, `[regex]`, and `[guid]`; .NET static methods, Add-Type, COM, and reflection are unavailable, while `-f`, property access, and core cmdlets work. Confined processes cannot use named pipes, so Node.js child processes with piped stdio fail with EPERM; inherited or ignored stdio and PowerShell pipelines work. After that denial, request the allowed exact-operation escalation or avoid output capture."},
	}
	for i, tc := range cases {
		if got := matcher.ObfuscateText(tc.input); got != tc.want {
			t.Errorf("case %d = %q, want %q", i, got, tc.want)
		}
	}
}

func TestAntigravityHarnessRulesRewriteClaudeStyles(t *testing.T) {
	raw, errRead := os.ReadFile("testdata/antigravity_harness_rules.yaml")
	if errRead != nil {
		t.Fatal(errRead)
	}
	cfg, errParse := config.ParseConfigBytes(raw)
	if errParse != nil {
		t.Fatal(errParse)
	}
	matcher := NewAntigravityExecutor(cfg).getSensitiveWordMatcher()
	if matcher == nil {
		t.Fatal("missing harness matcher")
	}
	cases := []struct{ input, want string }{
		{"You are an interactive CLI tool that helps users with software engineering tasks.", "Help the user complete software engineering tasks with the available tools."},
		{"You should work proactively and autonomously, executing immediately and minimizing interruptions.", "Act independently when authorized and avoid unnecessary interruptions."},
		{"# Proactive Style Active", "# Working approach"},
		{"Keep your responses short and direct while doing the work just as thoroughly.", "Respond briefly and directly while completing the work thoroughly."},
		{"# Concise Style Active", "# Response style"},
		{"In addition to software engineering tasks, you should provide educational insights about the codebase along the way.", "While completing the work, explain useful codebase details."},
		{"You should be clear and educational, providing helpful explanations while remaining focused on the task. Balance educational content with task completion. When providing insights, you may exceed typical length constraints, but remain focused and relevant.", "Explain relevant details clearly without losing focus on completing the task; use additional space when useful."},
		{"# Explanatory Style Active", "# Explanation style"},
		{"In addition to software engineering tasks, you should help users learn more about the codebase through hands-on practice and educational insights.", "Support learning about the codebase through practical exercises and explanations while doing the work."},
		{"You should be collaborative and encouraging. Balance task completion with learning by requesting user input for meaningful design decisions while handling routine implementation yourself.", "Collaborate constructively, ask for meaningful design decisions when needed, and handle routine implementation independently."},
		{"# Learning Style Active", "# Learning approach"},
		{"Claude responds tersely, leading with results and skipping preamble and narration", "Give succinct, outcome-first replies without preamble or narration."},
		{"Claude explains its implementation choices and codebase patterns", "Explain implementation choices and relevant codebase patterns."},
		{"Claude pauses and asks you to write small pieces of code for hands-on practice", "Invite short hands-on contributions when appropriate."},
	}
	for i, tc := range cases {
		if got := matcher.ObfuscateText(tc.input); got != tc.want {
			t.Errorf("case %d = %q, want %q", i, got, tc.want)
		}
	}
}
func TestAntigravityRewritesClaudeCodeIdentity(t *testing.T) {
	raw, errRead := os.ReadFile("testdata/antigravity_harness_rules.yaml")
	if errRead != nil {
		t.Fatal(errRead)
	}
	cfg, errParse := config.ParseConfigBytes(raw)
	if errParse != nil {
		t.Fatal(errParse)
	}
	matcher := NewAntigravityExecutor(cfg).getSensitiveWordMatcher()
	if matcher == nil {
		t.Fatal("missing harness matcher")
	}
	cases := []struct{ input, want string }{
		{"You are Claude Code, Anthropic's official CLI for Claude.", "You are an interactive CLI coding agent."},
		{"You are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK.", "You are an interactive CLI coding agent."},
		{"You are a Claude agent, built on Anthropic's Claude Agent SDK.", "You are an interactive CLI coding agent."},
		{"You are Claude Code, an AI assistant that orchestrates software engineering tasks across multiple workers.", "You are an agent that coordinates software engineering tasks across multiple workers."},
	}
	for i, tc := range cases {
		if got := matcher.ObfuscateText(tc.input); got != tc.want {
			t.Errorf("case %d = %q, want %q", i, got, tc.want)
		}
	}
}

func TestAntigravityHarnessRulesMatchAcrossZeroWidth(t *testing.T) {
	raw, errRead := os.ReadFile("testdata/antigravity_harness_rules.yaml")
	if errRead != nil {
		t.Fatal(errRead)
	}
	cfg, errParse := config.ParseConfigBytes(raw)
	if errParse != nil {
		t.Fatal(errParse)
	}
	matcher := NewAntigravityExecutor(cfg).getSensitiveWordMatcher()
	if matcher == nil {
		t.Fatal("missing harness matcher")
	}
	cases := []struct{ name, input, want string }{
		{"cgj-in-identity", "You are Claude Code, Anthro\u034Fpic's official CLI for Claude.", "You are an interactive CLI coding agent."},
		{"zwsp-in-identity", "You are \u200BClaude Code, Anthropic's official CLI for Claude.", "You are an interactive CLI coding agent."},
		{"cgj-in-harness-word", "DeepSeek\u034F Harness framework context", "agent runtime framework context"},
	}
	for _, tc := range cases {
		if got := matcher.ObfuscateText(tc.input); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
}
