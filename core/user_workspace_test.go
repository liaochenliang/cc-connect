package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type userWorkspaceTestPlatform struct {
	stubPlatformEngine
}

func (p *userWorkspaceTestPlatform) ReconstructReplyCtx(sessionKey string) (any, error) {
	return sessionKey, nil
}

type userWorkspacePathAgent struct {
	name      string
	workDir   string
	starts    *int
	listCalls *int
	listed    []AgentSessionInfo
}

func (a *userWorkspacePathAgent) Name() string { return a.name }
func (a *userWorkspacePathAgent) StartSession(context.Context, string) (AgentSession, error) {
	(*a.starts)++
	return newResultAgentSession(a.workDir), nil
}
func (a *userWorkspacePathAgent) ListSessions(context.Context) ([]AgentSessionInfo, error) {
	if a.listCalls != nil {
		(*a.listCalls)++
	}
	return a.listed, nil
}
func (a *userWorkspacePathAgent) Stop() error           { return nil }
func (a *userWorkspacePathAgent) GetWorkDir() string    { return a.workDir }
func (a *userWorkspacePathAgent) SetWorkDir(dir string) { a.workDir = dir }

type userWorkspaceDeleteAgent struct {
	stubDeleteAgent
	name      string
	deletedCh chan string
}

type userWorkspaceMemoryAgent struct {
	stubAgent
	name, projectFile, globalFile string
}

func (a *userWorkspaceMemoryAgent) Name() string              { return a.name }
func (a *userWorkspaceMemoryAgent) ProjectMemoryFile() string { return a.projectFile }
func (a *userWorkspaceMemoryAgent) GlobalMemoryFile() string  { return a.globalFile }

func (a *userWorkspaceDeleteAgent) Name() string { return a.name }
func (a *userWorkspaceDeleteAgent) DeleteSession(ctx context.Context, sessionID string) error {
	if err := a.stubDeleteAgent.DeleteSession(ctx, sessionID); err != nil {
		return err
	}
	a.deletedCh <- sessionID
	return nil
}

func newUserWorkspaceExecutionEngine(t *testing.T) (*Engine, *userWorkspaceTestPlatform, string, *int) {
	t.Helper()
	agentName := "user-workspace-" + strings.ReplaceAll(t.Name(), "/", "-")
	starts := 0
	RegisterAgent(agentName, func(opts map[string]any) (Agent, error) {
		workDir, _ := opts["work_dir"].(string)
		return &userWorkspacePathAgent{name: agentName, workDir: workDir, starts: &starts}, nil
	})
	platform := &userWorkspaceTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "wecom"}}
	baseAgent := &userWorkspacePathAgent{name: agentName, workDir: "global", starts: &starts}
	e := NewEngine("test", baseAgent, []Platform{platform}, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
	t.Cleanup(e.cancel)
	e.SetUserWorkspace(t.TempDir(), filepath.Join(t.TempDir(), "bindings.json"))
	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-1:alice", UserID: "alice"}
	workspace, err := e.prepareUserWorkspace(msg)
	if err != nil {
		t.Fatal(err)
	}
	return e, platform, workspace, &starts
}

func newUserWorkspaceCardContextEngine(t *testing.T) (*Engine, *userWorkspaceTestPlatform, string, *namedStubModelModeAgent, *namedStubModelModeAgent, *SessionManager) {
	t.Helper()
	agentName := "user-workspace-card-" + strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
	var workspaceAgent *namedStubModelModeAgent
	RegisterAgent(agentName, func(map[string]any) (Agent, error) {
		workspaceAgent = &namedStubModelModeAgent{name: agentName + "-workspace"}
		return workspaceAgent, nil
	})
	global := &namedStubModelModeAgent{name: agentName}
	platform := &userWorkspaceTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "wecom"}}
	e := NewEngine("test", global, []Platform{platform}, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
	t.Cleanup(e.cancel)
	e.SetUserWorkspace(t.TempDir(), filepath.Join(t.TempDir(), "bindings.json"))
	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-9:alice", UserID: "alice"}
	workspace, err := e.prepareUserWorkspace(msg)
	if err != nil {
		t.Fatal(err)
	}
	_, workspaceSessions, err := e.getOrCreateWorkspaceAgent(workspace)
	if err != nil {
		t.Fatal(err)
	}
	return e, platform, workspace, global, workspaceAgent, workspaceSessions
}

func newUserWorkspaceMemoryEngine(t *testing.T) (*Engine, *userWorkspaceTestPlatform, *Message, string, string, string, string) {
	t.Helper()
	agentName := "user-workspace-memory-" + strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
	globalDir := t.TempDir()
	globalProject := filepath.Join(globalDir, "PROJECT.md")
	globalGlobal := filepath.Join(globalDir, "GLOBAL.md")
	workspaceGlobal := filepath.Join(t.TempDir(), "WORKSPACE_GLOBAL.md")
	RegisterAgent(agentName, func(opts map[string]any) (Agent, error) {
		workDir, _ := opts["work_dir"].(string)
		return &userWorkspaceMemoryAgent{
			name:        agentName,
			projectFile: filepath.Join(workDir, "AGENTS.md"),
			globalFile:  workspaceGlobal,
		}, nil
	})
	global := &userWorkspaceMemoryAgent{name: agentName, projectFile: globalProject, globalFile: globalGlobal}
	platform := &userWorkspaceTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "wecom"}}
	e := NewEngine("test", global, []Platform{platform}, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
	t.Cleanup(e.cancel)
	e.SetUserWorkspace(t.TempDir(), filepath.Join(t.TempDir(), "bindings.json"))
	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-9:alice", UserID: "alice", ReplyCtx: "ctx"}
	workspace, err := e.prepareUserWorkspace(msg)
	if err != nil {
		t.Fatal(err)
	}
	return e, platform, msg, globalProject, globalGlobal, filepath.Join(workspace, "AGENTS.md"), workspaceGlobal
}

func configureUserSharedWorkspace(t *testing.T, e *Engine, name string) string {
	t.Helper()
	path := filepath.Join(e.baseDir, name)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := e.SetUserSharedWorkspaces([]string{name}); err != nil {
		t.Fatal(err)
	}
	return normalizeWorkspacePath(path)
}

func TestSetUserSharedWorkspacesValidatesDirectoriesAndCommands(t *testing.T) {
	baseDir := t.TempDir()
	e := NewEngine("test", nil, nil, "", LangChinese)
	e.SetUserWorkspace(baseDir, filepath.Join(t.TempDir(), "bindings.json"))

	if err := e.SetUserSharedWorkspaces([]string{"missing"}); err == nil {
		t.Fatal("missing shared workspace unexpectedly accepted")
	}
	if err := os.Mkdir(filepath.Join(baseDir, "new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := e.SetUserSharedWorkspaces([]string{"new"}); err == nil {
		t.Fatal("built-in command collision unexpectedly accepted")
	}
	if err := os.WriteFile(filepath.Join(baseDir, "filelab"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.SetUserSharedWorkspaces([]string{"filelab"}); err == nil {
		t.Fatal("ordinary file unexpectedly accepted")
	}
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(baseDir, "linklab")); err != nil {
		t.Fatal(err)
	}
	if err := e.SetUserSharedWorkspaces([]string{"linklab"}); err == nil {
		t.Fatal("symlink unexpectedly accepted")
	}
}

func TestSetUserSharedWorkspacesRejectsSymlinkBaseDir(t *testing.T) {
	target := t.TempDir()
	if err := os.Mkdir(filepath.Join(target, "medialab"), 0o755); err != nil {
		t.Fatal(err)
	}
	baseDir := filepath.Join(t.TempDir(), "base")
	if err := os.Symlink(target, baseDir); err != nil {
		t.Fatal(err)
	}
	e := NewEngine("test", nil, nil, "", LangChinese)
	e.SetUserWorkspace(baseDir, filepath.Join(t.TempDir(), "bindings.json"))
	if err := e.SetUserSharedWorkspaces([]string{"medialab"}); err == nil {
		t.Fatal("symlink base_dir unexpectedly accepted")
	}
}

func TestUserSharedWorkspaceSelectionClearsWhenBaseDirBecomesSymlink(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), "base")
	if err := os.Mkdir(baseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	e := NewEngine("test", nil, nil, "", LangChinese)
	e.SetUserWorkspace(baseDir, filepath.Join(t.TempDir(), "bindings.json"))
	configureUserSharedWorkspace(t, e, "medialab")
	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-1:alice", UserID: "alice"}
	if _, err := e.switchUserWorkspace(msg, "medialab"); err != nil {
		t.Fatal(err)
	}

	external := t.TempDir()
	if err := os.Mkdir(filepath.Join(external, "medialab"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(baseDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, baseDir); err != nil {
		t.Fatal(err)
	}

	workspace, err := e.prepareUserWorkspace(msg)
	var unavailable *userSharedWorkspaceUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Name != "medialab" {
		t.Fatalf("prepare workspace = %q, error = %v; want unavailable error", workspace, err)
	}
	if workspace != "" {
		t.Fatalf("workspace = %q, want no external path", workspace)
	}
	if got := e.selectedUserSharedWorkspace("alice"); got != "" {
		t.Fatalf("selection = %q, want cleared", got)
	}
	if binding := e.workspaceBindings.ListByProject("project:test")[workspaceChannelKey("wecom", "alice")]; binding != nil {
		t.Fatalf("binding = %#v, want removed", binding)
	}
}

func TestUserSharedWorkspaceSelectionUsesUserIDAcrossChats(t *testing.T) {
	baseDir := t.TempDir()
	e := NewEngine("test", nil, nil, "", LangChinese)
	e.SetUserWorkspace(baseDir, filepath.Join(t.TempDir(), "bindings.json"))
	shared := configureUserSharedWorkspace(t, e, "medialab")
	initial := &Message{Platform: "wecom", SessionKey: "wecom:group-1:alice", UserID: "alice"}
	if _, err := e.switchUserWorkspace(initial, "medialab"); err != nil {
		t.Fatal(err)
	}

	for _, sessionKey := range []string{"wecom:group-1:alice", "wecom:private-2:alice"} {
		msg := &Message{Platform: "wecom", SessionKey: sessionKey, UserID: "alice"}
		got, err := e.prepareUserWorkspace(msg)
		if err != nil || got != shared {
			t.Fatalf("prepareUserWorkspace(%q) = %q, %v; want %q", sessionKey, got, err, shared)
		}
	}

	bob := &Message{Platform: "wecom", SessionKey: "wecom:group-1:bob", UserID: "bob"}
	bobWorkspace, err := e.prepareUserWorkspace(bob)
	if err != nil {
		t.Fatal(err)
	}
	if bobWorkspace == shared {
		t.Fatal("bob unexpectedly inherited alice's shared workspace")
	}
}

func TestSwitchUserWorkspaceKeepsSelectionAndBindingConsistent(t *testing.T) {
	baseDir := t.TempDir()
	e := NewEngine("test", nil, nil, "", LangChinese)
	e.SetUserWorkspace(baseDir, "")
	shared := configureUserSharedWorkspace(t, e, "medialab")
	group := &Message{Platform: "wecom", SessionKey: "wecom:group-1:alice", UserID: "alice"}
	private := &Message{Platform: "wecom", SessionKey: "wecom:private-2:alice", UserID: "alice"}

	for range 200 {
		start := make(chan struct{})
		errs := make(chan error, 2)
		go func() {
			<-start
			_, err := e.switchUserWorkspace(group, "medialab")
			errs <- err
		}()
		go func() {
			<-start
			_, err := e.switchUserWorkspace(private, "")
			errs <- err
		}()
		close(start)
		for range 2 {
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		}

		want := shared
		if e.selectedUserSharedWorkspace("alice") == "" {
			var err error
			want, err = ensureUserWorkspaceDir(baseDir, "alice")
			if err != nil {
				t.Fatal(err)
			}
		}
		binding := e.workspaceBindings.ListByProject("project:test")[workspaceChannelKey("wecom", "alice")]
		if binding == nil || normalizeWorkspacePath(binding.Workspace) != want {
			t.Fatalf("selection = %q, binding = %#v; want workspace %q", e.selectedUserSharedWorkspace("alice"), binding, want)
		}
	}
}

func TestUserSharedWorkspaceSelectionClearsWhenDirectoryDisappears(t *testing.T) {
	baseDir := t.TempDir()
	e := NewEngine("test", nil, nil, "", LangChinese)
	e.SetUserWorkspace(baseDir, filepath.Join(t.TempDir(), "bindings.json"))
	shared := configureUserSharedWorkspace(t, e, "medialab")
	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-1:alice", UserID: "alice"}
	if _, err := e.switchUserWorkspace(msg, "medialab"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(shared); err != nil {
		t.Fatal(err)
	}
	_, err := e.prepareUserWorkspace(msg)
	var unavailable *userSharedWorkspaceUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Name != "medialab" {
		t.Fatalf("prepare error = %v, want medialab unavailable error", err)
	}
	if got := e.selectedUserSharedWorkspace("alice"); got != "" {
		t.Fatalf("selection = %q, want cleared", got)
	}
	want, err := ensureUserWorkspaceDir(baseDir, "alice")
	if err != nil {
		t.Fatal(err)
	}
	binding := e.workspaceBindings.ListByProject("project:test")[workspaceChannelKey("wecom", "alice")]
	if binding == nil || normalizeWorkspacePath(binding.Workspace) != want {
		t.Fatalf("binding = %#v, want /user workspace %q", binding, want)
	}
}

func TestUserSharedWorkspaceSelectionDoesNotRestoreFromBinding(t *testing.T) {
	baseDir := t.TempDir()
	storePath := filepath.Join(t.TempDir(), "bindings.json")
	shared := filepath.Join(baseDir, "medialab")
	if err := os.Mkdir(shared, 0o755); err != nil {
		t.Fatal(err)
	}

	first := NewEngine("test", nil, nil, "", LangChinese)
	t.Cleanup(first.cancel)
	first.SetUserWorkspace(baseDir, storePath)
	if err := first.SetUserSharedWorkspaces([]string{"medialab"}); err != nil {
		t.Fatal(err)
	}
	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-1:alice", UserID: "alice"}
	if got, err := first.switchUserWorkspace(msg, "medialab"); err != nil || got != normalizeWorkspacePath(shared) {
		t.Fatalf("first workspace = %q, %v", got, err)
	}

	restarted := NewEngine("test", nil, nil, "", LangChinese)
	t.Cleanup(restarted.cancel)
	restarted.SetUserWorkspace(baseDir, storePath)
	if err := restarted.SetUserSharedWorkspaces([]string{"medialab"}); err != nil {
		t.Fatal(err)
	}
	got, err := restarted.prepareUserWorkspace(msg)
	if err != nil {
		t.Fatal(err)
	}
	want, err := ensureUserWorkspaceDir(baseDir, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("workspace after restart = %q, want /user workspace %q", got, want)
	}
}

func TestEnsureUserWorkspaceDir(t *testing.T) {
	baseDir := t.TempDir()
	got, err := ensureUserWorkspaceDir(baseDir, "alice")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(baseDir, "616c696365")
	if got != want {
		t.Fatalf("workspace = %q, want %q", got, want)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %o, want 700", info.Mode().Perm())
	}
	again, err := ensureUserWorkspaceDir(baseDir, "alice")
	if err != nil || again != got {
		t.Fatalf("second lookup = %q, %v; want %q", again, err, got)
	}
}

func TestEnsureUserWorkspaceDirRejectsInvalidTargets(t *testing.T) {
	baseDir := t.TempDir()
	if _, err := ensureUserWorkspaceDir(baseDir, ""); err == nil {
		t.Fatal("empty UserID unexpectedly accepted")
	}
	if _, err := ensureUserWorkspaceDir(baseDir, strings.Repeat("x", 128)); err == nil {
		t.Fatal("overlong UserID unexpectedly accepted")
	}
	target := filepath.Join(baseDir, "616c696365")
	if err := os.Symlink(t.TempDir(), target); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureUserWorkspaceDir(baseDir, "alice"); err == nil {
		t.Fatal("symlink target unexpectedly accepted")
	}
	fileTarget := filepath.Join(baseDir, "626f62")
	if err := os.WriteFile(fileTarget, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureUserWorkspaceDir(baseDir, "bob"); err == nil {
		t.Fatal("file target unexpectedly accepted")
	}
	if _, err := ensureUserWorkspaceDir(filepath.Join(t.TempDir(), "missing"), "alice"); err == nil {
		t.Fatal("missing base_dir unexpectedly accepted")
	}
	baseLink := filepath.Join(t.TempDir(), "base-link")
	if err := os.Symlink(t.TempDir(), baseLink); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureUserWorkspaceDir(baseLink, "alice"); err == nil {
		t.Fatal("symlink base_dir unexpectedly accepted")
	}
}

func TestUserWorkspaceUsesUIDNotChatID(t *testing.T) {
	e := NewEngine("test", nil, nil, "", LangEnglish)
	e.SetUserWorkspace(t.TempDir(), filepath.Join(t.TempDir(), "bindings.json"))
	private := &Message{Platform: "wecom", SessionKey: "wecom:private-1:alice", UserID: "alice"}
	group := &Message{Platform: "wecom", SessionKey: "wecom:group-9:alice", UserID: "alice"}
	bob := &Message{Platform: "wecom", SessionKey: "wecom:group-9:bob", UserID: "bob"}
	privateDir, err := e.prepareUserWorkspace(private)
	if err != nil {
		t.Fatal(err)
	}
	groupDir, err := e.prepareUserWorkspace(group)
	if err != nil {
		t.Fatal(err)
	}
	bobDir, err := e.prepareUserWorkspace(bob)
	if err != nil {
		t.Fatal(err)
	}
	if privateDir != groupDir || privateDir == bobDir {
		t.Fatalf("workspace mapping private=%q group=%q bob=%q", privateDir, groupDir, bobDir)
	}
	if private.SessionKey == group.SessionKey {
		t.Fatal("chat sessions unexpectedly merged")
	}
}

func TestUserWorkspaceAttachmentPathAndSessionBinding(t *testing.T) {
	e := NewEngine("test", nil, nil, "", LangEnglish)
	e.SetUserWorkspace(t.TempDir(), filepath.Join(t.TempDir(), "bindings.json"))
	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-1:alice", UserID: "alice"}
	workspace, err := e.prepareUserWorkspace(msg)
	if err != nil {
		t.Fatal(err)
	}
	paths := SaveFilesToDisk(workspace, []FileAttachment{{FileName: "note.txt", Data: []byte("hello")}})
	wantPrefix := filepath.Join(workspace, ".cc-connect", "attachments") + string(filepath.Separator)
	if len(paths) != 1 || !strings.HasPrefix(paths[0], wantPrefix) {
		t.Fatalf("attachment paths = %#v, want prefix %q", paths, wantPrefix)
	}
	if got := e.workspaceBindingKeyForSession(msg.SessionKey); got != e.workspaceBindingKey(msg) {
		t.Fatalf("session binding key = %q, message key = %q", got, e.workspaceBindingKey(msg))
	}
}

func TestUserWorkspaceSessionRoutingUsesUIDBinding(t *testing.T) {
	const agentName = "user-workspace-routing-agent"
	RegisterAgent(agentName, func(map[string]any) (Agent, error) {
		return &namedTestAgent{name: agentName}, nil
	})

	baseAgent := &namedTestAgent{name: agentName}
	e := NewEngine("test", baseAgent, nil, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
	e.SetUserWorkspace(t.TempDir(), filepath.Join(t.TempDir(), "bindings.json"))
	msg := &Message{Platform: "wecom", SessionKey: "wecom:private-1:alice", UserID: "alice"}
	workspace, err := e.prepareUserWorkspace(msg)
	if err != nil {
		t.Fatal(err)
	}

	sessionKey := "wecom:group-9:alice"
	agent, sessions := e.sessionContextForKey(sessionKey)
	if agent == baseAgent || sessions == e.sessions {
		t.Fatal("session context fell back to the global workspace")
	}
	if got, want := e.interactiveKeyForSessionKey(sessionKey), workspace+":"+sessionKey; got != want {
		t.Fatalf("interactive key = %q, want %q", got, want)
	}

	resolved, err := e.resolveWorkspaceForSessionKey(e.platformForName("wecom"), sessionKey)
	if err != nil || resolved != workspace {
		t.Fatalf("scheduled workspace = %q, %v; want %q", resolved, err, workspace)
	}
	relayAgent, relaySessions, _, err := e.relayContextForSourceSessionKey("source", sessionKey)
	if err != nil {
		t.Fatal(err)
	}
	if relayAgent != agent || relaySessions != sessions {
		t.Fatal("relay context did not use the UID workspace")
	}
}

func TestUserWorkspaceStrictResolverRejectsTamperedBinding(t *testing.T) {
	e, _, _, _ := newUserWorkspaceExecutionEngine(t)
	sessionKey := "wecom:group-9:alice"
	channelKey := workspaceChannelKey("wecom", "alice")
	foreign := t.TempDir()
	e.workspaceBindings.Bind("project:test", channelKey, "alice", foreign)

	if _, err := e.resolveWorkspaceForSessionKey(e.platformForName("wecom"), sessionKey); err == nil {
		t.Fatal("mismatched project binding unexpectedly accepted")
	}
	foreignSessions := NewSessionManager("")
	foreignWorkspace := e.workspacePool.GetOrCreate(normalizeWorkspacePath(foreign))
	foreignWorkspace.agent = &userWorkspacePathAgent{name: e.agent.Name(), workDir: foreign, starts: new(int)}
	foreignWorkspace.sessions = foreignSessions
	if agent, sessions := e.sessionContextForKey(sessionKey); agent == foreignWorkspace.agent || sessions == foreignSessions {
		t.Fatal("tampered binding selected a foreign workspace session context")
	}
	if got := e.interactiveKeyForSessionKey(sessionKey); strings.HasPrefix(got, normalizeWorkspacePath(foreign)+":") {
		t.Fatalf("tampered binding selected foreign interactive key %q", got)
	}
}

func TestUserWorkspaceStrictResolverRejectsSharedBinding(t *testing.T) {
	e, _, workspace, _ := newUserWorkspaceExecutionEngine(t)
	channelKey := workspaceChannelKey("wecom", "alice")
	e.workspaceBindings.Unbind("project:test", channelKey)
	e.workspaceBindings.Bind(sharedWorkspaceBindingsKey, channelKey, "alice", workspace)
	if _, err := e.resolveWorkspaceForSessionKey(e.platformForName("wecom"), "wecom:group-9:alice"); err == nil {
		t.Fatal("shared binding unexpectedly accepted")
	}
}

func TestUserWorkspaceStrictResolverRequiresExactBinding(t *testing.T) {
	baseDir := t.TempDir()
	e := NewEngine("test", nil, nil, "", LangEnglish)
	e.SetUserWorkspace(baseDir, filepath.Join(t.TempDir(), "bindings.json"))
	workspace, err := ensureUserWorkspaceDir(baseDir, "alice")
	if err != nil {
		t.Fatal(err)
	}
	projectKey := "project:test"
	exactKey := workspaceChannelKey("wecom", "alice")
	e.workspaceBindings.Bind(projectKey, "alice", "alice", workspace)

	if _, err := e.resolveWorkspaceForSessionKey(e.platformForName("wecom"), "wecom:group-9:alice"); err == nil {
		t.Fatal("legacy raw UserID binding unexpectedly accepted")
	}
	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-9:alice", UserID: "alice"}
	if _, err := e.prepareUserWorkspace(msg); err != nil {
		t.Fatal(err)
	}
	exact := e.workspaceBindings.ListByProject(projectKey)[exactKey]
	if exact == nil || normalizeWorkspacePath(exact.Workspace) != workspace {
		t.Fatalf("exact binding = %#v, want workspace %q", exact, workspace)
	}
}

func TestUserWorkspaceStrictResolverRevalidatesTarget(t *testing.T) {
	e, _, workspace, _ := newUserWorkspaceExecutionEngine(t)
	sessionKey := "wecom:group-9:alice"
	if err := os.Chmod(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := e.resolveWorkspaceForSessionKey(e.platformForName("wecom"), sessionKey); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %o, want 700 after scheduled resolution", info.Mode().Perm())
	}

	if err := os.Remove(workspace); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := e.resolveWorkspaceForSessionKey(e.platformForName("wecom"), sessionKey); err == nil {
		t.Fatal("symlink workspace target unexpectedly accepted")
	}
}

func TestUserWorkspaceCommandContextRejectsInvalidOrUnresolvedWorkspace(t *testing.T) {
	e := NewEngine("test", &stubAgent{}, nil, "", LangEnglish)
	e.SetUserWorkspace(t.TempDir(), filepath.Join(t.TempDir(), "bindings.json"))
	p := &userWorkspaceTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "wecom"}}
	tests := []*Message{
		{Platform: "wecom", SessionKey: "", UserID: "alice"},
		{Platform: "wecom", SessionKey: "wecom:group-1:alice", UserID: ""},
		{Platform: "wecom", SessionKey: "wecom:group-1:alice", UserID: "alice"},
	}
	for _, msg := range tests {
		if _, _, _, _, err := e.commandContextWithWorkspace(p, msg); err == nil {
			t.Fatalf("command context unexpectedly accepted session=%q user=%q", msg.SessionKey, msg.UserID)
		}
	}
}

func TestUserWorkspaceHeartbeatUsesUIDWorkspace(t *testing.T) {
	e, platform, workspace, starts := newUserWorkspaceExecutionEngine(t)
	sessionKey := "wecom:group-1:alice"

	if err := e.ExecuteHeartbeat(sessionKey, "report", true); err != nil {
		t.Fatal(err)
	}
	if sent := strings.Join(platform.getSent(), "\n"); !strings.Contains(sent, workspace) {
		t.Fatalf("heartbeat output %q does not contain UID workspace %q", sent, workspace)
	}
	if *starts != 1 {
		t.Fatalf("agent starts = %d, want 1", *starts)
	}
	if history := e.sessions.GetOrCreateActive(sessionKey).HistoryLen(); history != 0 {
		t.Fatalf("global session history length = %d, want 0", history)
	}
	_, workspaceSessions, err := e.getOrCreateWorkspaceAgent(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if history := workspaceSessions.GetOrCreateActive(sessionKey).HistoryLen(); history == 0 {
		t.Fatal("workspace session history is empty")
	}
	e.interactiveMu.Lock()
	_, hasWorkspaceState := e.interactiveStates[workspace+":"+sessionKey]
	_, hasGlobalState := e.interactiveStates[sessionKey]
	e.interactiveMu.Unlock()
	if !hasWorkspaceState {
		t.Fatal("workspace interactive state is missing")
	}
	if hasGlobalState {
		t.Fatal("heartbeat created a global interactive state")
	}
}

func TestUserWorkspaceScheduledJobsUseUIDWorkspace(t *testing.T) {
	tests := []struct {
		name  string
		kind  string
		shell bool
	}{
		{name: "cron prompt", kind: "cron"},
		{name: "cron shell", kind: "cron", shell: true},
		{name: "timer prompt", kind: "timer"},
		{name: "timer shell", kind: "timer", shell: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, platform, workspace, starts := newUserWorkspaceExecutionEngine(t)
			var err error
			switch {
			case tt.kind == "cron" && tt.shell:
				err = e.ExecuteCronJob(&CronJob{ID: "cron-shell", SessionKey: "wecom:group-9:alice", Exec: "pwd"})
			case tt.kind == "cron":
				err = e.ExecuteCronJob(&CronJob{ID: "cron-prompt", SessionKey: "wecom:group-9:alice", Prompt: "report"})
			case tt.shell:
				err = e.ExecuteTimerJob(&TimerJob{ID: "timer-shell", SessionKey: "wecom:group-9:alice", Exec: "pwd"})
			default:
				err = e.ExecuteTimerJob(&TimerJob{ID: "timer-prompt", SessionKey: "wecom:group-9:alice", Prompt: "report"})
			}
			if err != nil {
				t.Fatal(err)
			}
			if sent := strings.Join(platform.getSent(), "\n"); !strings.Contains(sent, workspace) {
				t.Fatalf("scheduled output %q does not contain UID workspace %q", sent, workspace)
			}
			wantStarts := 1
			if tt.shell {
				wantStarts = 0
			}
			if *starts != wantStarts {
				t.Fatalf("agent starts = %d, want %d", *starts, wantStarts)
			}
		})
	}
}

func TestUserWorkspaceScheduledJobsRejectMismatchedWorkDir(t *testing.T) {
	tests := []struct {
		name  string
		kind  string
		shell bool
	}{
		{name: "cron prompt", kind: "cron"},
		{name: "cron shell", kind: "cron", shell: true},
		{name: "timer prompt", kind: "timer"},
		{name: "timer shell", kind: "timer", shell: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, platform, _, starts := newUserWorkspaceExecutionEngine(t)
			foreign := t.TempDir()
			sentinel := filepath.Join(t.TempDir(), "executed")
			var err error
			switch {
			case tt.kind == "cron" && tt.shell:
				err = e.ExecuteCronJob(&CronJob{ID: "cron-shell", SessionKey: "wecom:group-9:alice", Exec: "touch " + sentinel, WorkDir: foreign})
			case tt.kind == "cron":
				err = e.ExecuteCronJob(&CronJob{ID: "cron-prompt", SessionKey: "wecom:group-9:alice", Prompt: "report", WorkDir: foreign})
			case tt.shell:
				err = e.ExecuteTimerJob(&TimerJob{ID: "timer-shell", SessionKey: "wecom:group-9:alice", Exec: "touch " + sentinel, WorkDir: foreign})
			default:
				err = e.ExecuteTimerJob(&TimerJob{ID: "timer-prompt", SessionKey: "wecom:group-9:alice", Prompt: "report", WorkDir: foreign})
			}
			if err == nil {
				t.Fatal("mismatched work_dir unexpectedly executed")
			}
			if *starts != 0 {
				t.Fatalf("agent starts = %d, want 0", *starts)
			}
			if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
				t.Fatalf("shell sentinel stat error = %v, want not-exist", statErr)
			}
			if sent := platform.getSent(); len(sent) != 0 {
				t.Fatalf("messages sent before work_dir rejection: %#v", sent)
			}
		})
	}
}

func TestUserWorkspaceSendWorkDirMismatchIsRejected(t *testing.T) {
	e, platform, workspace, starts := newUserWorkspaceExecutionEngine(t)
	foreign := t.TempDir()
	sessionKey := "wecom:group-9:alice"
	e.bindSendWorkDir(sessionKey, foreign)
	e.handleMessage(platform, &Message{
		SessionKey: sessionKey,
		Platform:   "wecom",
		UserID:     "alice",
		Content:    "hello",
		ReplyCtx:   sessionKey,
	})
	if *starts != 0 {
		t.Fatalf("foreign agent starts = %d, want 0", *starts)
	}
	if e.workspacePool.Get(normalizeWorkspacePath(foreign)) != nil {
		t.Fatal("foreign send work_dir agent was created")
	}
	if _, err := os.Stat(workspace); err != nil {
		t.Fatalf("deterministic user workspace was not prepared: %v", err)
	}
}

func TestUserWorkspaceSendToSessionInWorkDirRejectsForeignWorkspace(t *testing.T) {
	e, platform, workspace, _ := newUserWorkspaceExecutionEngine(t)
	foreign := t.TempDir()
	sessionKey := "wecom:group-9:alice"
	err := e.SendToSessionInWorkDir(sessionKey, "hello", nil, nil, foreign, nil, false)
	if err == nil {
		t.Fatal("foreign public send work_dir unexpectedly accepted")
	}
	if e.workspacePool.Get(normalizeWorkspacePath(foreign)) != nil {
		t.Fatal("foreign public send created a workspace pool entry")
	}
	if got := e.sendWorkDirForSession(sessionKey); got != "" {
		t.Fatalf("send work_dir binding = %q, want empty", got)
	}
	if got := e.sessions.ListSessions(sessionKey); len(got) != 0 {
		t.Fatalf("global sessions = %#v, want empty", got)
	}
	if sent := platform.getSent(); len(sent) != 0 {
		t.Fatalf("platform sent = %#v, want empty", sent)
	}
	if binding := e.workspaceBindings.ListByProject("project:test")[workspaceChannelKey("wecom", "alice")]; binding == nil || normalizeWorkspacePath(binding.Workspace) != workspace {
		t.Fatalf("deterministic binding changed to %#v", binding)
	}
}

func TestUserWorkspaceCardDirActionsCannotMutateState(t *testing.T) {
	tests := []struct {
		name   string
		action func(workspace, foreign string) string
		setup  func(e *Engine, state *ProjectStateStore, interactiveKey, workspace, foreign string) string
	}{
		{
			name:   "select",
			action: func(_, foreign string) string { return "select " + foreign },
			setup:  func(*Engine, *ProjectStateStore, string, string, string) string { return "" },
		},
		{
			name:   "reset",
			action: func(_, _ string) string { return "reset" },
			setup: func(_ *Engine, state *ProjectStateStore, interactiveKey, _, foreign string) string {
				state.SetWorkspaceDirOverride(interactiveKey, foreign)
				return foreign
			},
		},
		{
			name:   "prev",
			action: func(_, _ string) string { return "prev" },
			setup: func(e *Engine, _ *ProjectStateStore, _, workspace, foreign string) string {
				history := NewDirHistory(t.TempDir())
				history.Add(e.name, foreign)
				history.Add(e.name, workspace)
				e.SetDirHistory(history)
				return ""
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _, workspace, _ := newUserWorkspaceExecutionEngine(t)
			state := NewProjectStateStore(filepath.Join(t.TempDir(), "project-state.json"))
			e.SetProjectStateStore(state)
			foreign := t.TempDir()
			sessionKey := "wecom:group-9:alice"
			interactiveKey := workspace + ":" + sessionKey
			want := tt.setup(e, state, interactiveKey, workspace, foreign)
			e.executeCardAction("/dir", tt.action(workspace, foreign), sessionKey)
			if got := state.WorkspaceDirOverride(interactiveKey); got != want {
				t.Fatalf("workspace dir override = %q, want unchanged %q", got, want)
			}
		})
	}
}

func TestUserWorkspaceCardNavRejectsMissingBindingBeforeGlobalRead(t *testing.T) {
	starts, listCalls := 0, 0
	global := &userWorkspacePathAgent{name: "global", workDir: "global", starts: &starts, listCalls: &listCalls}
	e := NewEngine("test", global, nil, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
	e.SetUserWorkspace(t.TempDir(), filepath.Join(t.TempDir(), "bindings.json"))
	sessionKey := "wecom:group-9:alice"
	card := e.handleCardNav("nav:/list", sessionKey)
	if card == nil || !strings.Contains(card.RenderText(), "Workspace resolution error") {
		t.Fatalf("card = %#v, want workspace resolution error", card)
	}
	if listCalls != 0 {
		t.Fatalf("global ListSessions calls = %d, want 0", listCalls)
	}
	if got := e.sessions.ListSessions(sessionKey); len(got) != 0 {
		t.Fatalf("global sessions = %#v, want empty", got)
	}
}

func TestUserWorkspaceCardActionRejectsTamperedBindingBeforeGlobalState(t *testing.T) {
	starts, listCalls := 0, 0
	global := &userWorkspacePathAgent{
		name:      "global",
		workDir:   "global",
		starts:    &starts,
		listCalls: &listCalls,
		listed:    []AgentSessionInfo{{ID: "global-session", Summary: "global"}},
	}
	e := NewEngine("test", global, nil, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
	baseDir := t.TempDir()
	e.SetUserWorkspace(baseDir, filepath.Join(t.TempDir(), "bindings.json"))
	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-9:alice", UserID: "alice"}
	if _, err := e.prepareUserWorkspace(msg); err != nil {
		t.Fatal(err)
	}
	e.workspaceBindings.Bind("project:test", workspaceChannelKey("wecom", "alice"), "alice", t.TempDir())
	state := NewProjectStateStore(filepath.Join(t.TempDir(), "project-state.json"))
	e.SetProjectStateStore(state)

	e.executeCardAction("/switch", "1", msg.SessionKey)
	e.executeCardAction("/dir", "select "+t.TempDir(), msg.SessionKey)
	if listCalls != 0 {
		t.Fatalf("global ListSessions calls = %d, want 0", listCalls)
	}
	if got := e.sessions.ListSessions(msg.SessionKey); len(got) != 0 {
		t.Fatalf("global sessions = %#v, want empty", got)
	}
	if got := state.WorkspaceDirOverride(msg.SessionKey); got != "" {
		t.Fatalf("global workspace dir override = %q, want empty", got)
	}
}

func TestUserWorkspaceCardContextFailsClosedWhenAgentFactoryFails(t *testing.T) {
	agentName := "user-workspace-failing-card-factory"
	RegisterAgent(agentName, func(map[string]any) (Agent, error) {
		return nil, errors.New("factory failed")
	})
	global := &namedStubModelModeAgent{name: agentName}
	global.SetReasoningEffort("low")
	e := NewEngine("test", global, nil, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
	e.SetUserWorkspace(t.TempDir(), filepath.Join(t.TempDir(), "bindings.json"))
	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-9:alice", UserID: "alice"}
	workspace, err := e.prepareUserWorkspace(msg)
	if err != nil {
		t.Fatal(err)
	}
	state := NewProjectStateStore(filepath.Join(t.TempDir(), "project-state.json"))
	e.SetProjectStateStore(state)
	state.SetWorkspaceDirOverride(workspace+":"+msg.SessionKey, workspace)

	card := e.handleCardNav("nav:/reasoning", msg.SessionKey)
	if card == nil || !strings.Contains(card.RenderText(), "Workspace resolution error") {
		t.Fatalf("card = %#v, want workspace resolution error", card)
	}
	e.executeCardAction("/reasoning", "high", msg.SessionKey)
	if got := global.GetReasoningEffort(); got != "low" {
		t.Fatalf("global reasoning = %q, want unchanged low", got)
	}
	if got := e.sessions.ListSessions(msg.SessionKey); len(got) != 0 {
		t.Fatalf("global sessions = %#v, want empty", got)
	}
	if got := state.WorkspaceDirOverride(workspace + ":" + msg.SessionKey); got != workspace {
		t.Fatalf("project state override = %q, want unchanged %q", got, workspace)
	}
}

func TestUserWorkspaceCardNavUsesWorkspaceAgentState(t *testing.T) {
	e, _, _, global, workspaceAgent, _ := newUserWorkspaceCardContextEngine(t)
	global.SetReasoningEffort("low")
	global.SetMode("default")
	global.SetProviders([]ProviderConfig{{Name: "global-provider"}})
	global.SetActiveProvider("global-provider")
	workspaceAgent.SetReasoningEffort("high")
	workspaceAgent.SetMode("yolo")
	workspaceAgent.SetProviders([]ProviderConfig{{Name: "workspace-provider"}})
	workspaceAgent.SetActiveProvider("workspace-provider")
	sessionKey := "wecom:group-9:alice"

	reasoning := e.handleCardNav("nav:/reasoning", sessionKey).RenderText()
	if !strings.Contains(reasoning, "high") || strings.Contains(reasoning, "effort: low") {
		t.Fatalf("reasoning card = %q, want workspace state", reasoning)
	}
	mode := e.handleCardNav("nav:/mode", sessionKey).RenderText()
	if !strings.Contains(mode, "▶ **YOLO**") {
		t.Fatalf("mode card = %q, want workspace mode selected", mode)
	}
	provider := e.handleCardNav("nav:/provider", sessionKey).RenderText()
	if !strings.Contains(provider, "workspace-provider") || strings.Contains(provider, "global-provider") {
		t.Fatalf("provider card = %q, want workspace provider", provider)
	}
}

func TestUserWorkspaceCardActionsMutateOnlyWorkspaceContext(t *testing.T) {
	e, _, _, global, workspaceAgent, workspaceSessions := newUserWorkspaceCardContextEngine(t)
	global.SetReasoningEffort("low")
	global.SetMode("yolo")
	global.SetProviders([]ProviderConfig{{Name: "global-provider"}, {Name: "workspace-two"}})
	global.SetActiveProvider("global-provider")
	workspaceAgent.SetReasoningEffort("high")
	workspaceAgent.SetMode("yolo")
	workspaceAgent.SetProviders([]ProviderConfig{{Name: "workspace-one"}, {Name: "workspace-two"}})
	workspaceAgent.SetActiveProvider("workspace-one")
	providerSaves := 0
	e.SetProviderSaveFunc(func(string) error {
		providerSaves++
		return nil
	})
	sessionKey := "wecom:group-9:alice"

	e.handleCardNav("act:/reasoning 2", sessionKey)
	e.handleCardNav("act:/mode default", sessionKey)
	e.handleCardNav("act:/provider workspace-two", sessionKey)

	if got := workspaceAgent.GetReasoningEffort(); got != "medium" {
		t.Fatalf("workspace reasoning = %q, want medium", got)
	}
	if got := workspaceAgent.GetMode(); got != "default" {
		t.Fatalf("workspace mode = %q, want default", got)
	}
	if active := workspaceAgent.GetActiveProvider(); active == nil || active.Name != "workspace-two" {
		t.Fatalf("workspace provider = %#v, want workspace-two", active)
	}
	if got := global.GetReasoningEffort(); got != "low" {
		t.Fatalf("global reasoning = %q, want unchanged low", got)
	}
	if got := global.GetMode(); got != "yolo" {
		t.Fatalf("global mode = %q, want unchanged yolo", got)
	}
	if active := global.GetActiveProvider(); active == nil || active.Name != "global-provider" {
		t.Fatalf("global provider = %#v, want unchanged global-provider", active)
	}
	if providerSaves != 0 {
		t.Fatalf("global provider saves = %d, want 0", providerSaves)
	}
	if got := e.sessions.ListSessions(sessionKey); len(got) != 0 {
		t.Fatalf("global sessions = %#v, want empty", got)
	}
	if got := workspaceSessions.ListSessions(sessionKey); len(got) == 0 {
		t.Fatal("workspace session was not reset after card actions")
	}
}

func TestUserWorkspacePendingProviderCompletionUsesWorkspaceAgent(t *testing.T) {
	e, platform, workspace, global, workspaceAgent, _ := newUserWorkspaceCardContextEngine(t)
	global.SetProviders([]ProviderConfig{{Name: "global-provider"}})
	workspaceAgent.SetProviders([]ProviderConfig{{Name: "workspace-provider"}})
	providerSaves := 0
	e.SetProviderAddSaveFunc(func(ProviderConfig) error {
		providerSaves++
		return nil
	})
	sessionKey := "wecom:group-9:alice"
	e.setPendingProviderAdd(sessionKey, &pendingProviderAddState{phase: "other"})
	msg := &Message{Platform: "wecom", SessionKey: sessionKey, UserID: "alice", ReplyCtx: "ctx"}

	if !e.handlePendingProviderAdd(platform, msg, "workspace-added secret", workspace+":"+sessionKey) {
		t.Fatal("pending provider input was not handled")
	}
	if providers := workspaceAgent.ListProviders(); len(providers) != 2 || providers[1].Name != "workspace-added" {
		t.Fatalf("workspace providers = %#v, want appended provider", providers)
	}
	if providers := global.ListProviders(); len(providers) != 1 || providers[0].Name != "global-provider" {
		t.Fatalf("global providers = %#v, want unchanged", providers)
	}
	if providerSaves != 0 {
		t.Fatalf("global provider add saves = %d, want 0", providerSaves)
	}
}

func TestUserWorkspacePlainProviderAddRemoveDoNotPersistGlobalConfig(t *testing.T) {
	e, platform, _, global, workspaceAgent, _ := newUserWorkspaceCardContextEngine(t)
	global.SetProviders([]ProviderConfig{{Name: "global-provider"}})
	workspaceAgent.SetProviders([]ProviderConfig{{Name: "workspace-remove"}})
	addSaves, removeSaves := 0, 0
	e.SetProviderAddSaveFunc(func(ProviderConfig) error {
		addSaves++
		return nil
	})
	e.SetProviderRemoveSaveFunc(func(string) error {
		removeSaves++
		return nil
	})
	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-9:alice", UserID: "alice", ReplyCtx: "ctx"}

	e.cmdProvider(platform, msg, []string{"add", "workspace-added", "secret"})
	e.cmdProvider(platform, msg, []string{"remove", "workspace-remove"})

	if providers := workspaceAgent.ListProviders(); len(providers) != 1 || providers[0].Name != "workspace-added" {
		t.Fatalf("workspace providers = %#v, want runtime add/remove only", providers)
	}
	if providers := global.ListProviders(); len(providers) != 1 || providers[0].Name != "global-provider" {
		t.Fatalf("global providers = %#v, want unchanged", providers)
	}
	if addSaves != 0 || removeSaves != 0 {
		t.Fatalf("global provider callbacks add=%d remove=%d, want 0/0", addSaves, removeSaves)
	}
}

func TestUserWorkspaceAPIServerRejectsEverySocketEndpoint(t *testing.T) {
	api, err := NewAPIServer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = api.listener.Close()
		api.Stop()
	})

	platform := &userWorkspaceTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "wecom"}}
	engine := NewEngine("test", &stubAgent{}, []Platform{platform}, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
	t.Cleanup(engine.cancel)
	engine.SetUserWorkspace(t.TempDir(), filepath.Join(t.TempDir(), "bindings.json"))
	sessionKey := "wecom:group-9:alice"
	engine.interactiveStates[sessionKey] = &interactiveState{platform: platform, replyCtx: "ctx"}

	cronStore, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cronJobs := []*CronJob{
		{ID: "cron-edit", Project: "test", SessionKey: sessionKey, CronExpr: "0 6 * * *", Prompt: "edit", Description: "before", Enabled: true},
		{ID: "cron-del", Project: "test", SessionKey: sessionKey, CronExpr: "0 6 * * *", Prompt: "delete", Enabled: true},
		{ID: "cron-exec", Project: "test", SessionKey: sessionKey, CronExpr: "0 6 * * *", Prompt: "execute", Enabled: false},
	}
	for _, job := range cronJobs {
		if err := cronStore.Add(job); err != nil {
			t.Fatal(err)
		}
	}
	cronScheduler := NewCronScheduler(cronStore)
	t.Cleanup(cronScheduler.Stop)

	timerStore, err := NewTimerStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	timerJobs := []*TimerJob{
		{ID: "timer-info", Project: "test", SessionKey: sessionKey, ScheduledAt: time.Now().Add(time.Hour), Prompt: "info"},
		{ID: "timer-del", Project: "test", SessionKey: sessionKey, ScheduledAt: time.Now().Add(time.Hour), Prompt: "delete"},
	}
	for _, job := range timerJobs {
		if err := timerStore.Add(job); err != nil {
			t.Fatal(err)
		}
	}
	timerScheduler := NewTimerScheduler(timerStore)
	t.Cleanup(timerScheduler.Stop)

	relay := NewRelayManager("")
	relay.Bind("wecom", "existing-chat", map[string]string{"test": "Test", "other": "Other"})
	api.SetRelayManager(relay)
	api.SetCronScheduler(cronScheduler)
	api.SetTimerScheduler(timerScheduler)
	api.RegisterEngine("test", engine)

	tests := []struct {
		name, method, path, body string
	}{
		{"send", http.MethodPost, "/send", `{"project":"test","session_key":"wecom:group-9:alice","message":"blocked"}`},
		{"sessions", http.MethodGet, "/sessions", ""},
		{"cron add", http.MethodPost, "/cron/add", `{"project":"test","session_key":"wecom:group-9:alice","cron_expr":"0 7 * * *","prompt":"blocked"}`},
		{"cron list", http.MethodGet, "/cron/list?project=test", ""},
		{"cron info", http.MethodGet, "/cron/info?id=cron-edit", ""},
		{"cron edit", http.MethodPost, "/cron/edit", `{"id":"cron-edit","field":"description","value":"changed"}`},
		{"cron del", http.MethodPost, "/cron/del", `{"id":"cron-del"}`},
		{"cron exec", http.MethodPost, "/cron/exec", `{"id":"cron-exec"}`},
		{"cron run", http.MethodPost, "/cron/run", `{"id":"cron-exec"}`},
		{"timer add", http.MethodPost, "/timer/add", `{"project":"test","session_key":"wecom:group-9:alice","delay":"1h","prompt":"blocked"}`},
		{"timer list", http.MethodGet, "/timer/list?project=test", ""},
		{"timer info", http.MethodGet, "/timer/info?id=timer-info", ""},
		{"timer del", http.MethodPost, "/timer/del", `{"id":"timer-del"}`},
		{"relay send", http.MethodPost, "/relay/send", `{"from":"test","to":"other","session_key":"wecom:group-9:alice","message":"blocked"}`},
		{"relay bind", http.MethodPost, "/relay/bind", `{"platform":"wecom","chat_id":"new-chat","bots":{"test":"Test","other":"Other"}}`},
		{"relay binding", http.MethodGet, "/relay/binding?chat_id=existing-chat", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()
			api.mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
			}
		})
	}

	if sent := platform.getSent(); len(sent) != 0 {
		t.Fatalf("socket API sent messages: %#v", sent)
	}
	if jobs := cronStore.List(); len(jobs) != len(cronJobs) || cronStore.Get("cron-del") == nil || cronStore.Get("cron-edit").Description != "before" {
		t.Fatalf("cron store changed: %#v", jobs)
	}
	if jobs := timerStore.List(); len(jobs) != len(timerJobs) || timerStore.Get("timer-del") == nil {
		t.Fatalf("timer store changed: %#v", jobs)
	}
	if relay.GetBinding("new-chat") != nil {
		t.Fatal("relay binding changed")
	}
}

func TestUserWorkspaceWebhookIsDisabled(t *testing.T) {
	e, _, _, starts := newUserWorkspaceExecutionEngine(t)
	server := NewWebhookServer(0, "", "/hook")
	server.RegisterEngine("test", e)
	workDir := t.TempDir()
	tests := []struct {
		name string
		req  WebhookRequest
	}{
		{name: "prompt", req: WebhookRequest{Project: "test", SessionKey: "wecom:group-1:alice", Prompt: "blocked", Silent: true}},
		{name: "exec", req: WebhookRequest{Project: "test", SessionKey: "wecom:group-1:alice", Exec: "touch executed", WorkDir: workDir, Silent: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(tt.req)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(body))
			rec := httptest.NewRecorder()

			server.handleHook(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
	if *starts != 0 {
		t.Fatalf("agent starts = %d, want 0", *starts)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(workDir, "executed")); !os.IsNotExist(err) {
		t.Fatalf("webhook exec was not blocked: %v", err)
	}
}

func TestLegacyWebhookRemainsEnabled(t *testing.T) {
	starts := 0
	platform := &userWorkspaceTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "wecom"}}
	agent := &userWorkspacePathAgent{name: "legacy", workDir: "legacy", starts: &starts}
	e := NewEngine("test", agent, []Platform{platform}, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
	t.Cleanup(e.cancel)
	server := NewWebhookServer(0, "", "/hook")
	server.RegisterEngine("test", e)
	body, err := json.Marshal(WebhookRequest{Project: "test", SessionKey: "wecom:group-1:alice", Prompt: "allowed", Silent: true})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleHook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(platform.getSent()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if sent := platform.getSent(); len(sent) == 0 {
		t.Fatal("legacy webhook did not execute prompt")
	}
}

func TestLegacyAPIServerRoutesRemainEnabled(t *testing.T) {
	api, err := NewAPIServer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = api.listener.Close()
		api.Stop()
	})
	api.RegisterEngine("test", NewEngine("test", &stubAgent{}, nil, "", LangEnglish))
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestUserWorkspaceMemoryDisablesGlobalButKeepsProjectMemory(t *testing.T) {
	e, platform, msg, globalProject, globalGlobal, workspaceProject, workspaceGlobal := newUserWorkspaceMemoryEngine(t)
	if err := os.WriteFile(globalProject, []byte("global project original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalGlobal, []byte("global memory secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workspaceGlobal, []byte("workspace global secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	e.cmdMemory(platform, msg, []string{"add", "workspace", "note"})
	projectData, err := os.ReadFile(workspaceProject)
	if err != nil || !strings.Contains(string(projectData), "workspace note") {
		t.Fatalf("workspace project memory = %q, %v", projectData, err)
	}
	platform.clearSent()
	e.cmdMemory(platform, msg, []string{"global"})
	e.cmdMemory(platform, msg, []string{"global", "add", "blocked", "note"})
	replies := strings.Join(platform.getSent(), "\n")
	if strings.Count(replies, e.i18n.T(MsgMemoryNotSupported)) != 2 {
		t.Fatalf("global memory replies = %q, want two not-supported replies", replies)
	}
	if data, _ := os.ReadFile(globalProject); string(data) != "global project original" {
		t.Fatalf("global project memory changed: %q", data)
	}
	if data, _ := os.ReadFile(globalGlobal); string(data) != "global memory secret" {
		t.Fatalf("global memory changed: %q", data)
	}
	if data, _ := os.ReadFile(workspaceGlobal); string(data) != "workspace global secret" {
		t.Fatalf("workspace global memory changed: %q", data)
	}
}

func TestUserWorkspaceSetupCommandsUseWorkspaceAgentMemory(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Engine, Platform, *Message)
	}{
		{"cron setup", func(e *Engine, p Platform, msg *Message) {
			e.cronScheduler = &CronScheduler{}
			e.cmdCronSetup(p, msg)
		}},
		{"bind setup", func(e *Engine, p Platform, msg *Message) {
			e.cmdBindSetup(p, msg)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, platform, msg, globalProject, _, workspaceProject, _ := newUserWorkspaceMemoryEngine(t)
			if err := os.WriteFile(globalProject, []byte("global original"), 0o600); err != nil {
				t.Fatal(err)
			}
			tt.run(e, platform, msg)
			workspaceData, err := os.ReadFile(workspaceProject)
			if err != nil || !strings.Contains(string(workspaceData), ccConnectInstructionMarker) {
				t.Fatalf("workspace setup memory = %q, %v", workspaceData, err)
			}
			if data, _ := os.ReadFile(globalProject); string(data) != "global original" {
				t.Fatalf("global project memory changed: %q", data)
			}
		})
	}
}

func TestLegacyPlainProviderAddRemovePersistGlobalConfig(t *testing.T) {
	agent := &namedStubModelModeAgent{name: "legacy-provider-agent"}
	agent.SetProviders([]ProviderConfig{{Name: "legacy-remove"}})
	platform := &userWorkspaceTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "wecom"}}
	e := NewEngine("test", agent, []Platform{platform}, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
	t.Cleanup(e.cancel)
	addSaves, removeSaves := 0, 0
	e.SetProviderAddSaveFunc(func(ProviderConfig) error {
		addSaves++
		return nil
	})
	e.SetProviderRemoveSaveFunc(func(string) error {
		removeSaves++
		return nil
	})
	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-9:alice", UserID: "alice", ReplyCtx: "ctx"}

	e.cmdProvider(platform, msg, []string{"add", "legacy-added", "secret"})
	e.cmdProvider(platform, msg, []string{"remove", "legacy-remove"})

	if providers := agent.ListProviders(); len(providers) != 1 || providers[0].Name != "legacy-added" {
		t.Fatalf("legacy providers = %#v, want runtime add/remove", providers)
	}
	if addSaves != 1 || removeSaves != 1 {
		t.Fatalf("legacy provider callbacks add=%d remove=%d, want 1/1", addSaves, removeSaves)
	}
}

func TestUserWorkspaceProviderAddAndLinkUseWorkspaceAgent(t *testing.T) {
	e, _, _, global, workspaceAgent, _ := newUserWorkspaceCardContextEngine(t)
	global.SetProviders([]ProviderConfig{{Name: "global-provider"}})
	workspaceAgent.SetProviders([]ProviderConfig{{Name: "workspace-provider"}})

	globalPresetsCache.mu.Lock()
	oldData, oldFetchedAt, oldURL := globalPresetsCache.data, globalPresetsCache.fetchedAt, globalPresetsCache.url
	globalPresetsCache.data = &ProviderPresetsResponse{}
	globalPresetsCache.fetchedAt = time.Now()
	globalPresetsCache.mu.Unlock()
	t.Cleanup(func() {
		globalPresetsCache.mu.Lock()
		globalPresetsCache.data, globalPresetsCache.fetchedAt, globalPresetsCache.url = oldData, oldFetchedAt, oldURL
		globalPresetsCache.mu.Unlock()
	})

	var agentTypes []string
	e.SetListGlobalProvidersFunc(func(agentType string) ([]ProviderConfig, error) {
		agentTypes = append(agentTypes, agentType)
		return []ProviderConfig{{Name: "linked-provider"}}, nil
	})
	providerRefSaves := 0
	e.SetProviderRefsSaveFunc(func([]string) error {
		providerRefSaves++
		return nil
	})
	sessionKey := "wecom:group-9:alice"

	e.handleCardNav("nav:/provider/add", sessionKey)
	e.handleCardNav("act:/provider/add-other", sessionKey)
	if pending := e.getPendingProviderAdd(sessionKey); pending == nil || pending.phase != "other" {
		t.Fatalf("pending provider state = %#v, want workspace other flow", pending)
	}
	e.handleCardNav("act:/provider/link linked-provider", sessionKey)

	if len(agentTypes) < 2 {
		t.Fatalf("global provider queries = %#v, want add and link queries", agentTypes)
	}
	for _, agentType := range agentTypes {
		if agentType != workspaceAgent.Name() {
			t.Fatalf("global provider query agent type = %q, want %q", agentType, workspaceAgent.Name())
		}
	}
	if providers := workspaceAgent.ListProviders(); len(providers) != 2 || providers[1].Name != "linked-provider" {
		t.Fatalf("workspace providers = %#v, want linked provider", providers)
	}
	if providers := global.ListProviders(); len(providers) != 1 || providers[0].Name != "global-provider" {
		t.Fatalf("global providers = %#v, want unchanged", providers)
	}
	if providerRefSaves != 0 {
		t.Fatalf("global provider_refs saves = %d, want 0", providerRefSaves)
	}
}

func TestUserWorkspaceDeleteSubmitKeepsCapturedContextAfterBindingChange(t *testing.T) {
	agentName := "user-workspace-delete-context"
	workspaceDeleted := make(chan string, 1)
	var workspaceAgent *userWorkspaceDeleteAgent
	RegisterAgent(agentName, func(map[string]any) (Agent, error) {
		workspaceAgent = &userWorkspaceDeleteAgent{
			stubDeleteAgent: stubDeleteAgent{stubListAgent: stubListAgent{sessions: []AgentSessionInfo{{ID: "session-1", Summary: "workspace"}}}},
			name:            agentName,
			deletedCh:       workspaceDeleted,
		}
		return workspaceAgent, nil
	})
	globalDeleted := make(chan string, 1)
	global := &userWorkspaceDeleteAgent{
		stubDeleteAgent: stubDeleteAgent{stubListAgent: stubListAgent{sessions: []AgentSessionInfo{{ID: "session-1", Summary: "global"}}}},
		name:            agentName,
		deletedCh:       globalDeleted,
	}
	platform := &userWorkspaceTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "wecom"}}
	e := NewEngine("test", global, []Platform{platform}, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
	t.Cleanup(e.cancel)
	e.SetUserWorkspace(t.TempDir(), filepath.Join(t.TempDir(), "bindings.json"))
	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-9:alice", UserID: "alice"}
	if _, err := e.prepareUserWorkspace(msg); err != nil {
		t.Fatal(err)
	}
	agent, sessions, interactiveKey, err := e.cardContextForSessionKey(msg.SessionKey)
	if err != nil {
		t.Fatal(err)
	}
	dm := e.getOrCreateDeleteModeState(msg.SessionKey, platform, "ctx")
	dm.selectedIDs["session-1"] = struct{}{}

	channelKey := workspaceChannelKey("wecom", "alice")
	e.workspaceBindings.mu.Lock()
	e.executeCardActionWithContext("/delete-mode", "submit", msg.SessionKey, interactiveKey, agent, sessions)
	e.workspaceBindings.bindings["project:test"][channelKey].Workspace = t.TempDir()
	e.workspaceBindings.mu.Unlock()

	select {
	case deleted := <-workspaceDeleted:
		if deleted != "session-1" {
			t.Fatalf("workspace deleted session = %q, want session-1", deleted)
		}
	case deleted := <-globalDeleted:
		t.Fatalf("global agent deleted %q after binding changed", deleted)
	case <-time.After(time.Second):
		t.Fatal("workspace deletion did not complete")
	}
	deadline := time.Now().Add(time.Second)
	for {
		e.interactiveMu.Lock()
		state := e.interactiveStates[interactiveKey]
		e.interactiveMu.Unlock()
		phase := ""
		if state != nil {
			state.mu.Lock()
			if state.deleteMode != nil {
				phase = state.deleteMode.phase
			}
			state.mu.Unlock()
		}
		if phase == "result" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("delete mode did not reach result phase")
		}
		time.Sleep(time.Millisecond)
	}
	if got := e.sessions.ListSessions(msg.SessionKey); len(got) != 0 {
		t.Fatalf("global sessions = %#v, want empty after workspace deletion", got)
	}
}

func TestUserWorkspacePreparationErrorUsesI18n(t *testing.T) {
	p := &userWorkspaceTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "wecom"}}
	e := NewEngine("test", nil, []Platform{p}, filepath.Join(t.TempDir(), "sessions.json"), LangChinese)
	e.SetUserWorkspace(filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "bindings.json"))
	e.handleMessage(p, &Message{
		Platform:   "wecom",
		SessionKey: "wecom:group-9:alice",
		UserID:     "alice",
		Content:    "hello",
		ReplyCtx:   "ctx",
	})
	if sent := strings.Join(p.getSent(), "\n"); !strings.Contains(sent, "工作区解析错误") {
		t.Fatalf("preparation error reply = %q, want localized workspace error", sent)
	}
}

func TestUserWorkspaceCommandRejectsBindingMutation(t *testing.T) {
	e, platform, workspace, _ := newUserWorkspaceExecutionEngine(t)
	if err := os.Mkdir(filepath.Join(e.baseDir, "other"), 0o700); err != nil {
		t.Fatal(err)
	}
	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-9:alice", UserID: "alice", ReplyCtx: "ctx"}
	e.handleWorkspaceCommand(platform, msg, []string{"bind", "other"})
	binding := e.workspaceBindings.Lookup("project:test", workspaceChannelKey("wecom", "alice"))
	if binding == nil || normalizeWorkspacePath(binding.Workspace) != workspace {
		t.Fatalf("user workspace binding mutated to %#v", binding)
	}
}

func TestUserWorkspaceDirCommandRejectsPersistentOverride(t *testing.T) {
	e, platform, workspace, _ := newUserWorkspaceExecutionEngine(t)
	state := NewProjectStateStore(filepath.Join(t.TempDir(), "project-state.json"))
	e.SetProjectStateStore(state)
	foreign := t.TempDir()
	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-9:alice", UserID: "alice", ReplyCtx: "ctx"}
	e.cmdDir(platform, msg, []string{foreign})
	interactiveKey := workspace + ":" + msg.SessionKey
	if got := state.WorkspaceDirOverride(interactiveKey); got != "" {
		t.Fatalf("workspace dir override = %q, want empty", got)
	}
}

func TestUserWorkspaceCronCommandsEnforceSessionOwnership(t *testing.T) {
	e, platform, _, _ := newUserWorkspaceExecutionEngine(t)
	bob := &Message{Platform: "wecom", SessionKey: "wecom:group-9:bob", UserID: "bob"}
	if _, err := e.prepareUserWorkspace(bob); err != nil {
		t.Fatal(err)
	}

	store, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	jobs := []*CronJob{
		{ID: "alice-list", Project: e.name, SessionKey: "wecom:group-9:alice", CronExpr: "0 6 * * *", Prompt: "alice task", Enabled: true},
		{ID: "alice-other-project", Project: "other", SessionKey: "wecom:group-9:alice", CronExpr: "0 6 * * *", Prompt: "other project task", Enabled: true},
		{ID: "bob-exec", Project: e.name, SessionKey: bob.SessionKey, CronExpr: "0 6 * * *", Prompt: "bob task", Enabled: true},
		{ID: "bob-delete", Project: e.name, SessionKey: bob.SessionKey, CronExpr: "0 6 * * *", Prompt: "bob task", Enabled: true},
		{ID: "bob-enable", Project: e.name, SessionKey: bob.SessionKey, CronExpr: "0 6 * * *", Prompt: "bob task", Enabled: false},
		{ID: "bob-disable", Project: e.name, SessionKey: bob.SessionKey, CronExpr: "0 6 * * *", Prompt: "bob task", Enabled: true},
		{ID: "bob-mute", Project: e.name, SessionKey: bob.SessionKey, CronExpr: "0 6 * * *", Prompt: "bob task", Enabled: true},
		{ID: "bob-unmute", Project: e.name, SessionKey: bob.SessionKey, CronExpr: "0 6 * * *", Prompt: "bob task", Enabled: true, Mute: true},
		{ID: "bob-card", Project: e.name, SessionKey: bob.SessionKey, CronExpr: "0 6 * * *", Prompt: "bob task", Enabled: true},
	}
	for _, job := range jobs {
		if err := store.Add(job); err != nil {
			t.Fatal(err)
		}
	}
	scheduler := NewCronScheduler(store)
	scheduler.RegisterEngine(e.name, e)
	e.SetCronScheduler(scheduler)
	t.Cleanup(scheduler.Stop)
	alice := &Message{SessionKey: "wecom:group-9:alice", UserID: "alice", ReplyCtx: "ctx"}

	e.cmdCronList(platform, alice)
	listed := strings.Join(platform.getSent(), "\n")
	if !strings.Contains(listed, "alice-list") || strings.Contains(listed, "bob-exec") || strings.Contains(listed, "alice-other-project") {
		t.Fatalf("Alice cron list = %q, want only Alice's jobs", listed)
	}
	if card := e.renderCronCard(alice.SessionKey, alice.UserID); strings.Contains(card.RenderText(), "alice-other-project") {
		t.Fatalf("Alice cron card leaked another project: %q", card.RenderText())
	}

	platform.clearSent()
	e.cmdCronExec(platform, alice, []string{"bob-exec"})
	if reply := strings.Join(platform.getSent(), "\n"); !strings.Contains(reply, "not found") {
		t.Fatalf("Alice cron exec reply = %q, want not found", reply)
	}

	e.cmdCronDel(platform, alice, []string{"bob-delete"})
	e.cmdCronDel(platform, alice, []string{"alice-other-project"})
	e.cmdCronToggle(platform, alice, []string{"bob-enable"}, true)
	e.cmdCronToggle(platform, alice, []string{"bob-disable"}, false)
	e.cmdCronMute(platform, alice, []string{"bob-mute"}, true)
	e.cmdCronMute(platform, alice, []string{"bob-unmute"}, false)
	if store.Get("bob-delete") == nil || store.Get("alice-other-project") == nil || store.Get("bob-enable").Enabled || !store.Get("bob-disable").Enabled || store.Get("bob-mute").Mute || !store.Get("bob-unmute").Mute {
		t.Fatal("Alice text command mutated Bob's cron job")
	}

	e.executeCardAction("/cron", "disable bob-card", alice.SessionKey)
	e.executeCardAction("/cron", "mute bob-card", alice.SessionKey)
	e.executeCardAction("/cron", "delete bob-card", alice.SessionKey)
	if job := store.Get("bob-card"); job == nil || !job.Enabled || job.Mute {
		t.Fatalf("Alice card action mutated Bob's cron job: %#v", job)
	}
}

func TestUserWorkspaceTimerCommandsEnforceSessionOwnership(t *testing.T) {
	e, platform, _, _ := newUserWorkspaceExecutionEngine(t)
	store, err := NewTimerStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	jobs := []*TimerJob{
		{ID: "alice-list", Project: e.name, SessionKey: "wecom:group-9:alice", ScheduledAt: time.Now().Add(time.Hour), Prompt: "alice task"},
		{ID: "alice-other-project", Project: "other", SessionKey: "wecom:group-9:alice", ScheduledAt: time.Now().Add(time.Hour), Prompt: "other project task"},
		{ID: "bob-delete", Project: e.name, SessionKey: "wecom:group-9:bob", ScheduledAt: time.Now().Add(time.Hour), Prompt: "bob task"},
		{ID: "bob-mute", Project: e.name, SessionKey: "wecom:group-9:bob", ScheduledAt: time.Now().Add(time.Hour), Prompt: "bob task"},
		{ID: "bob-unmute", Project: e.name, SessionKey: "wecom:group-9:bob", ScheduledAt: time.Now().Add(time.Hour), Prompt: "bob task", Mute: true},
		{ID: "bob-card", Project: e.name, SessionKey: "wecom:group-9:bob", ScheduledAt: time.Now().Add(time.Hour), Prompt: "bob task"},
	}
	for _, job := range jobs {
		if err := store.Add(job); err != nil {
			t.Fatal(err)
		}
	}
	scheduler := NewTimerScheduler(store)
	e.SetTimerScheduler(scheduler)
	t.Cleanup(scheduler.Stop)
	alice := &Message{SessionKey: "wecom:group-9:alice", UserID: "alice", ReplyCtx: "ctx"}

	e.cmdTimerList(platform, alice)
	listed := strings.Join(platform.getSent(), "\n")
	if !strings.Contains(listed, "alice-list") || strings.Contains(listed, "bob-delete") || strings.Contains(listed, "alice-other-project") {
		t.Fatalf("Alice timer list = %q, want only Alice's jobs", listed)
	}
	if card := e.renderTimerCard(alice.SessionKey, alice.UserID); strings.Contains(card.RenderText(), "alice-other-project") {
		t.Fatalf("Alice timer card leaked another project: %q", card.RenderText())
	}

	e.cmdTimerDel(platform, alice, []string{"bob-delete"})
	e.cmdTimerDel(platform, alice, []string{"alice-other-project"})
	e.cmdTimerMute(platform, alice, []string{"bob-mute"}, true)
	e.cmdTimerMute(platform, alice, []string{"bob-unmute"}, false)
	if store.Get("bob-delete") == nil || store.Get("alice-other-project") == nil || store.Get("bob-mute").Mute || !store.Get("bob-unmute").Mute {
		t.Fatal("Alice text command mutated Bob's timer job")
	}

	e.executeCardAction("/timer", "mute bob-card", alice.SessionKey)
	e.executeCardAction("/timer", "delete bob-card", alice.SessionKey)
	if job := store.Get("bob-card"); job == nil || job.Mute {
		t.Fatalf("Alice card action mutated Bob's timer job: %#v", job)
	}
}
