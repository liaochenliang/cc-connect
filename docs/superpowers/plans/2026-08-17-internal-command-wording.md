# Internal Command Wording Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace product-specific unknown-command wording with localized “internal command” wording while preserving Agent forwarding and the existing quiet-mode acknowledgements used by the WeCom special deployment.

**Architecture:** Keep the existing `MsgUnknownCommand` key and `handleCommand` fallthrough unchanged. Update only its five translations, add exact translation coverage, strengthen the existing engine-matrix behavior test, and rerun the existing quiet idle/busy regressions.

**Tech Stack:** Go, standard `testing` package, existing CC-Connect i18n and release-local test harness

---

## File Mapping

- Modify: `core/i18n.go` — replace the five `MsgUnknownCommand` translations.
- Modify: `core/i18n_test.go` — assert the exact localized notice in every supported language.
- Modify: `tests/release_local/engine_matrix/engine_matrix_test.go` — require “internal command” at the engine boundary while preserving Agent forwarding.
- No change: `core/engine.go` — unknown-command fallthrough and quiet idle/busy acknowledgement logic already match the approved behavior.

### Task 1: Lock the new wording with failing tests

**Files:**
- Modify: `core/i18n_test.go`
- Modify: `tests/release_local/engine_matrix/engine_matrix_test.go:292-301`

- [ ] **Step 1: Run GitNexus impact analysis before editing test symbols**

Run:

```text
gitnexus_impact(repo="cc-connect", target="TestUnknownSlashCommandNotifiesThenFallsThroughToAgent", file_path="tests/release_local/engine_matrix/engine_matrix_test.go", direction="upstream", includeTests=true)
```

Expected: review the reported risk and direct dependents; stop and report before editing if risk is HIGH or CRITICAL.

- [ ] **Step 2: Add the exact translation regression**

Add to `core/i18n_test.go`:

```go
func TestI18n_UnknownCommandUsesInternalWording(t *testing.T) {
	tests := []struct {
		lang Language
		want string
	}{
		{LangEnglish, "`/not-a-command` is not an internal command, forwarding to agent..."},
		{LangChinese, "`/not-a-command` 不是内部命令，已转发给 Agent 处理..."},
		{LangTraditionalChinese, "`/not-a-command` 不是內部命令，已轉發給 Agent 處理..."},
		{LangJapanese, "`/not-a-command` は内部コマンドではありません。エージェントに転送します..."},
		{LangSpanish, "`/not-a-command` no es un comando interno, reenviando al agente..."},
	}

	for _, tt := range tests {
		t.Run(string(tt.lang), func(t *testing.T) {
			got := NewI18n(tt.lang).Tf(MsgUnknownCommand, "/not-a-command")
			if got != tt.want {
				t.Fatalf("unknown command notice = %q, want %q", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 3: Strengthen the engine-matrix assertion**

In `TestUnknownSlashCommandNotifiesThenFallsThroughToAgent`, replace:

```go
platform.waitTextContaining(t, "forwarding")
```

with:

```go
platform.waitTextContaining(t, "internal command")
```

Keep the existing assertion that the original slash request reaches the Agent.

- [ ] **Step 4: Run both tests and verify RED**

Run:

```bash
go test ./core -run TestI18n_UnknownCommandUsesInternalWording -count=1
```

Expected: FAIL because the current translations still say `cc-connect command`.

Run:

```bash
go test ./tests/release_local/engine_matrix -run TestUnknownSlashCommandNotifiesThenFallsThroughToAgent -count=1
```

Expected: FAIL because the current notice does not contain `internal command`.

### Task 2: Replace the five translations

**Files:**
- Modify: `core/i18n.go:976-982`

- [ ] **Step 1: Run GitNexus impact analysis for the translated message**

Run:

```text
gitnexus_impact(repo="cc-connect", target="MsgUnknownCommand", file_path="core/i18n.go", direction="upstream", includeTests=true)
```

Expected: review all direct consumers; the known runtime consumer is `Engine.handleCommand`.

- [ ] **Step 2: Apply the minimum implementation**

Replace the `MsgUnknownCommand` translations with:

```go
MsgUnknownCommand: {
	LangEnglish:            "`%s` is not an internal command, forwarding to agent...",
	LangChinese:            "`%s` 不是内部命令，已转发给 Agent 处理...",
	LangTraditionalChinese: "`%s` 不是內部命令，已轉發給 Agent 處理...",
	LangJapanese:           "`%s` は内部コマンドではありません。エージェントに転送します...",
	LangSpanish:            "`%s` no es un comando interno, reenviando al agente...",
},
```

Do not change `MsgStarting`, `MsgMessageQueued`, or `core/engine.go`.

- [ ] **Step 3: Format and verify GREEN**

Run:

```bash
gofmt -w core/i18n_test.go tests/release_local/engine_matrix/engine_matrix_test.go
go test ./core -run TestI18n_UnknownCommandUsesInternalWording -count=1
go test ./tests/release_local/engine_matrix -run TestUnknownSlashCommandNotifiesThenFallsThroughToAgent -count=1
```

Expected: both test commands PASS.

- [ ] **Step 4: Verify the existing WeCom-special acknowledgement behavior**

Run:

```bash
go test ./core -run 'TestHandleMessage_Quiet(SendsStartingForStreamingCardPlatform|BusySessionKeepsQueuedReply)$' -count=1
```

Expected: PASS, proving idle quiet messages still send `⏳ 处理中...` and busy messages only send `📬 消息已收到，将在当前任务完成后处理。`.

- [ ] **Step 5: Commit the focused implementation**

Run:

```bash
git add core/i18n.go core/i18n_test.go tests/release_local/engine_matrix/engine_matrix_test.go
git diff --cached --check
git commit -m "fix(core): describe unknown slash commands as internal"
```

Expected: one implementation commit containing only the three listed files.

### Task 3: Full verification and review

**Files:**
- Review only: `core/i18n.go`, `core/i18n_test.go`, `tests/release_local/engine_matrix/engine_matrix_test.go`

- [ ] **Step 1: Run affected and full verification**

Run:

```bash
go test ./core ./tests/release_local/engine_matrix
go test ./...
go build ./...
```

Expected: all commands exit with status 0.

- [ ] **Step 2: Check scope and graph impact**

Run:

```bash
git diff --check 4a72c09..HEAD
git show --stat --oneline 4a72c09..HEAD
```

Then run:

```text
gitnexus_detect_changes(repo="cc-connect", scope="compare", base_ref="4a72c09")
```

Expected: changes are limited to the i18n notice and its tests, with no unexpected execution-flow impact.

- [ ] **Step 3: Review and simplify**

Use `code-review`, `requesting-code-review`, and `code-simplifier` with this review brief:

```text
Requirement: unknown slash commands say “internal command” in all five languages, still notify the user, and still reach the Agent. Existing quiet idle/busy acknowledgements must remain unchanged.
Scope: 4a72c09..HEAD.
```

Expected: fix all Critical and Important findings, rerun Task 3 Step 1 after any correction, and add no abstraction or dependency.

### Task 4: Rebuild and deploy the WeCom special version

**Files in `/Users/liaochenliang/Code/k8s-agent`:**
- Modify: `k8s-codex/cc-connect/source.commit`
- Modify: `k8s-codex/README.md`
- Modify: `k8s-codex/cc-connect/README.md`
- Regenerate: `k8s-codex/cc-connect/cc-connect-v1.4.1-user-workspace.1-linux-amd64.tar.gz`
- Regenerate: `k8s-codex/cc-connect/cc-connect-v1.4.1-user-workspace.1-linux-amd64.tar.gz.sha256`
- Regenerate: `k8s-codex/dep-codex.yml`

- [ ] **Step 1: Confirm the special deployment still selects quiet mode**

Run:

```bash
grep -Fxq 'mode = "quiet"' /Users/liaochenliang/Code/k8s-agent/k8s-codex/cc-connect/config.toml
```

Expected: exit status 0. This existing setting enables `⏳ 处理中...` for idle messages and the queue acknowledgement for busy messages.

- [ ] **Step 2: Update the pinned source commit without including dirty-tree files**

Run:

```bash
git -C /Users/liaochenliang/Code/cc-connect rev-parse HEAD
```

Use `apply_patch` to replace the previous pinned hash `2822db570accc50594f9f3c6b6de8bd62374c76b` with the returned full hash in:

```text
/Users/liaochenliang/Code/k8s-agent/k8s-codex/cc-connect/source.commit
/Users/liaochenliang/Code/k8s-agent/k8s-codex/README.md
/Users/liaochenliang/Code/k8s-agent/k8s-codex/cc-connect/README.md
```

Also replace the documented short hash `2822db5` in `k8s-codex/cc-connect/README.md` with the first seven characters of the returned hash.

Expected: `source.commit` and both README files refer to the implementation commit. Existing uncommitted `AGENTS.md` and `CLAUDE.md` changes in the source repository remain uncommitted and are excluded because the build uses `git archive <commit>`.

- [ ] **Step 3: Rebuild and verify the pinned Linux artifact**

Run:

```bash
cd /Users/liaochenliang/Code/k8s-agent
CC_CONNECT_SOURCE=/Users/liaochenliang/Code/cc-connect \
  bash k8s-codex/cc-connect/build-user-workspace.sh
bash k8s-codex/tests/test_cc_connect.sh
```

Expected: the reproducible build and contract test PASS, and the archive checksum changes to match the new source commit.

- [ ] **Step 4: Commit only the artifact update**

Run:

```bash
git -C /Users/liaochenliang/Code/k8s-agent add \
  k8s-codex/README.md \
  k8s-codex/cc-connect/README.md \
  k8s-codex/cc-connect/source.commit \
  k8s-codex/cc-connect/cc-connect-v1.4.1-user-workspace.1-linux-amd64.tar.gz \
  k8s-codex/cc-connect/cc-connect-v1.4.1-user-workspace.1-linux-amd64.tar.gz.sha256
git -C /Users/liaochenliang/Code/k8s-agent diff --cached --check
git -C /Users/liaochenliang/Code/k8s-agent commit -m "chore(k8s-codex): update cc-connect artifact"
```

Expected: the commit contains only the five listed deployment files.

- [ ] **Step 5: Build, push, and deploy the special image**

Run:

```bash
cd /Users/liaochenliang/Code/k8s-agent
test -s k8s-codex/lab-ai-file-staging/.env
docker info >/dev/null
kubectl config current-context
bash k8s-codex/k8s-codex.sh
```

Expected: image build and push succeed, Kubernetes rollout reports `deployment/autoagent-codex-cli successfully rolled out`, and `dep-codex.yml` contains the new timestamped image tag.

- [ ] **Step 6: Verify the running Pod**

Run:

```bash
namespace=gz3-sdk-wrapper
pod="$(kubectl get pods -n "$namespace" -l app=autoagent-codex-cli \
  -o jsonpath='{.items[0].metadata.name}')"
kubectl get pod "$pod" -n "$namespace" \
  -o jsonpath='{.metadata.name}{" ready="}{.status.containerStatuses[0].ready}{" restarts="}{.status.containerStatuses[0].restartCount}{"\n"}'
kubectl exec "$pod" -n "$namespace" -- sh -ceu '
grep -Fxq '\''mode = "quiet"'\'' /usr/local/share/cc-connect/config.toml
grep -aFq '\''不是内部命令，已转发给 Agent 处理...'\'' /usr/local/bin/cc-connect
grep -aFq '\''⏳ 处理中...'\'' /usr/local/bin/cc-connect
grep -aFq '\''📬 消息已收到，将在当前任务完成后处理。'\'' /usr/local/bin/cc-connect
'
kubectl logs "$pod" -n "$namespace" --since=10m
```

Expected: the Pod is Ready with zero restarts, the installed config remains quiet, the binary contains all three approved notices, and logs contain no startup failure.

- [ ] **Step 7: Commit the deployed image tag**

Run:

```bash
git -C /Users/liaochenliang/Code/k8s-agent add k8s-codex/dep-codex.yml
git -C /Users/liaochenliang/Code/k8s-agent diff --cached --check
git -C /Users/liaochenliang/Code/k8s-agent commit -m "chore(k8s-codex): deploy internal command wording"
```

Expected: the deployment repository is clean and the running image tag matches `dep-codex.yml`.
