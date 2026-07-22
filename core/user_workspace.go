package core

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func encodeUserWorkspaceID(userID string) (string, error) {
	if userID == "" {
		return "", fmt.Errorf("user-workspace: UserID is empty")
	}
	encoded := hex.EncodeToString([]byte(userID))
	if len(encoded) > 255 {
		return "", fmt.Errorf("user-workspace: encoded UserID is too long")
	}
	return encoded, nil
}

func validateUserWorkspaceBaseDir(baseDir string) error {
	baseInfo, err := os.Lstat(baseDir)
	if err != nil {
		return fmt.Errorf("user-workspace: inspect base_dir: %w", err)
	}
	if !baseInfo.IsDir() || baseInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("user-workspace: base_dir must be a real directory")
	}
	return nil
}

func ensureUserWorkspaceDir(baseDir, userID string) (string, error) {
	if err := validateUserWorkspaceBaseDir(baseDir); err != nil {
		return "", err
	}
	encoded, err := encodeUserWorkspaceID(userID)
	if err != nil {
		return "", err
	}
	target := filepath.Join(baseDir, encoded)
	if err := os.Mkdir(target, 0o700); err != nil && !os.IsExist(err) {
		return "", fmt.Errorf("user-workspace: create directory: %w", err)
	}
	info, err := os.Lstat(target)
	if err != nil {
		return "", fmt.Errorf("user-workspace: inspect directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("user-workspace: target must be a real directory")
	}
	if err := os.Chmod(target, 0o700); err != nil {
		return "", fmt.Errorf("user-workspace: set directory mode: %w", err)
	}
	return normalizeWorkspacePath(target), nil
}

func (e *Engine) SetUserWorkspace(baseDir, bindingStorePath string) {
	e.SetMultiWorkspace(baseDir, bindingStorePath)
	e.userWorkspace = true
	e.userWorkspaceMu.Lock()
	e.userSharedWorkspaces = make(map[string]string)
	e.userWorkspaceSelections = make(map[string]string)
	e.userWorkspaceMu.Unlock()
}

type userSharedWorkspaceUnavailableError struct {
	Name string
	Err  error
}

func (e *userSharedWorkspaceUnavailableError) Error() string {
	return fmt.Sprintf("user-workspace: shared workspace %q unavailable: %v", e.Name, e.Err)
}

func (e *userSharedWorkspaceUnavailableError) Unwrap() error { return e.Err }

func (e *Engine) SetUserSharedWorkspaces(names []string) error {
	if !e.userWorkspace {
		return fmt.Errorf("user-workspace: shared workspaces require user-workspace mode")
	}
	if err := validateUserWorkspaceBaseDir(e.baseDir); err != nil {
		return err
	}
	workspaces := make(map[string]string, len(names))
	for _, name := range names {
		name = strings.ToLower(name)
		if name == "user" || matchPrefix(name, builtinCommands) != "" {
			return fmt.Errorf("user-workspace: shared workspace name %q conflicts with a command", name)
		}
		path := filepath.Join(e.baseDir, name)
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("user-workspace: inspect shared workspace %q: %w", name, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("user-workspace: shared workspace %q must be a real directory", name)
		}
		workspaces[name] = normalizeWorkspacePath(path)
	}
	e.userWorkspaceMu.Lock()
	e.userSharedWorkspaces = workspaces
	e.userWorkspaceSelections = make(map[string]string)
	e.userWorkspaceMu.Unlock()
	return nil
}

func (e *Engine) selectedUserSharedWorkspace(userID string) string {
	e.userWorkspaceMu.RLock()
	defer e.userWorkspaceMu.RUnlock()
	return e.userWorkspaceSelections[userID]
}

func (e *Engine) matchUserWorkspaceSelectionCommand(cmd string) (string, bool) {
	if !e.userWorkspace {
		return "", false
	}
	cmd = strings.ToLower(strings.TrimPrefix(cmd, "/"))
	e.userWorkspaceMu.RLock()
	defer e.userWorkspaceMu.RUnlock()
	if len(e.userSharedWorkspaces) == 0 {
		return "", false
	}
	if cmd == "user" {
		return "", true
	}
	_, ok := e.userSharedWorkspaces[cmd]
	return cmd, ok
}

func (e *Engine) userHasBusyWorkspaceSession(userID string) bool {
	if e.workspacePool == nil {
		return false
	}
	// ponytail: workspace/session counts are small; add an index only if this scan is measured hot.
	for _, state := range e.workspacePool.All() {
		state.mu.Lock()
		sessions := state.sessions
		state.mu.Unlock()
		if sessions == nil {
			continue
		}
		idToKey, activeIDs := sessions.SessionKeyMap()
		for sessionID, sessionKey := range idToKey {
			if !activeIDs[sessionID] || userIDFromWeComSessionKey(sessionKey) != userID {
				continue
			}
			if session := sessions.FindByID(sessionID); session != nil && session.Busy() {
				return true
			}
		}
	}
	return false
}

func (e *Engine) handleUserWorkspaceSelectionCommand(p Platform, msg *Message, cmd string, args []string) bool {
	target, ok := e.matchUserWorkspaceSelectionCommand(cmd)
	if !ok {
		return false
	}
	if len(args) != 0 {
		e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgUserWsSwitchUsage, "/"+cmd))
		return true
	}
	current := e.selectedUserSharedWorkspace(msg.UserID)
	if current == target {
		if target == "" {
			e.reply(p, msg.ReplyCtx, e.i18n.T(MsgUserWsAlreadyUser))
		} else {
			e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgUserWsAlreadyShared, target))
		}
		return true
	}
	if e.userHasBusyWorkspaceSession(msg.UserID) {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgUserWsSwitchBusy))
		return true
	}
	if _, err := e.switchUserWorkspace(msg, target); err != nil {
		var unavailable *userSharedWorkspaceUnavailableError
		if errors.As(err, &unavailable) {
			e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgUserWsSharedUnavailable, unavailable.Name))
		} else {
			e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgWsResolutionError, err))
		}
		return true
	}
	if target == "" {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgUserWsSwitchedUser))
	} else {
		e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgUserWsSwitchedShared, target))
	}
	return true
}

func (e *Engine) resolveSelectedUserWorkspaceLocked(userID string) (string, error) {
	name := e.userWorkspaceSelections[userID]
	path := e.userSharedWorkspaces[name]
	if name == "" {
		return ensureUserWorkspaceDir(e.baseDir, userID)
	}
	if err := validateUserWorkspaceBaseDir(e.baseDir); err != nil {
		delete(e.userWorkspaceSelections, userID)
		e.workspaceBindings.Unbind("project:"+e.name, workspaceChannelKey("wecom", userID))
		return "", &userSharedWorkspaceUnavailableError{Name: name, Err: err}
	}
	info, err := os.Lstat(path)
	if err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}
	if err == nil {
		err = fmt.Errorf("path is not a real directory")
	}
	if resetErr := e.resetUserWorkspaceSelectionLocked(userID); resetErr != nil {
		err = fmt.Errorf("%v; reset to /user: %w", err, resetErr)
	}
	return "", &userSharedWorkspaceUnavailableError{Name: name, Err: err}
}

func (e *Engine) resetUserWorkspaceSelectionLocked(userID string) error {
	delete(e.userWorkspaceSelections, userID)
	workspace, err := ensureUserWorkspaceDir(e.baseDir, userID)
	if err != nil {
		return err
	}
	e.bindUserWorkspaceLocked("wecom", userID, workspace)
	return nil
}

func (e *Engine) prepareUserWorkspace(msg *Message) (string, error) {
	if msg == nil || msg.Platform != "wecom" {
		return "", fmt.Errorf("user-workspace: authenticated WeCom message required")
	}
	e.userWorkspaceMu.Lock()
	defer e.userWorkspaceMu.Unlock()
	return e.prepareUserWorkspaceLocked(msg)
}

func (e *Engine) prepareUserWorkspaceLocked(msg *Message) (string, error) {
	workspace, err := e.resolveSelectedUserWorkspaceLocked(msg.UserID)
	if err != nil {
		return "", err
	}
	e.bindUserWorkspaceLocked(msg.Platform, msg.UserID, workspace)
	return workspace, nil
}

func (e *Engine) bindUserWorkspaceLocked(platform, userID, workspace string) {
	projectKey := "project:" + e.name
	channelKey := workspaceChannelKey(platform, userID)
	if binding := e.workspaceBindings.ListByProject(projectKey)[channelKey]; binding == nil || normalizeWorkspacePath(binding.Workspace) != workspace {
		e.workspaceBindings.Bind(projectKey, channelKey, userID, workspace)
	}
}

func (e *Engine) switchUserWorkspace(msg *Message, name string) (string, error) {
	if msg == nil || msg.Platform != "wecom" {
		return "", fmt.Errorf("user-workspace: authenticated WeCom message required")
	}
	e.userWorkspaceMu.Lock()
	defer e.userWorkspaceMu.Unlock()
	if name == "" {
		delete(e.userWorkspaceSelections, msg.UserID)
	} else {
		e.userWorkspaceSelections[msg.UserID] = name
	}
	return e.prepareUserWorkspaceLocked(msg)
}

func userIDFromWeComSessionKey(sessionKey string) string {
	parts := strings.SplitN(sessionKey, ":", 3)
	if len(parts) != 3 || parts[0] != "wecom" {
		return ""
	}
	return parts[2]
}

func (e *Engine) workspaceChannelID(msg *Message) string {
	if e.userWorkspace {
		return msg.UserID
	}
	return effectiveChannelID(msg)
}

func (e *Engine) workspaceBindingKey(msg *Message) string {
	if e.userWorkspace {
		return workspaceChannelKey(msg.Platform, msg.UserID)
	}
	return effectiveWorkspaceChannelKey(msg)
}

func (e *Engine) workspaceBindingKeyForSession(sessionKey string) string {
	if e.userWorkspace {
		return workspaceChannelKey(extractPlatformName(sessionKey), userIDFromWeComSessionKey(sessionKey))
	}
	return extractWorkspaceChannelKey(sessionKey)
}

func (e *Engine) resolveWorkspaceForSessionKey(p Platform, sessionKey string) (string, error) {
	if !e.userWorkspace {
		workspace, _, err := e.resolveWorkspace(p, extractChannelID(sessionKey))
		return workspace, err
	}
	userID := userIDFromWeComSessionKey(sessionKey)
	if userID == "" {
		return "", fmt.Errorf("user-workspace: invalid WeCom session key")
	}
	e.userWorkspaceMu.Lock()
	defer e.userWorkspaceMu.Unlock()
	workspace, err := e.resolveSelectedUserWorkspaceLocked(userID)
	if err != nil {
		return "", err
	}
	channelKey := workspaceChannelKey("wecom", userID)
	binding := e.workspaceBindings.ListByProject("project:" + e.name)[channelKey]
	if binding == nil {
		return "", fmt.Errorf("user-workspace: no workspace binding for session %q", sessionKey)
	}
	if normalizeWorkspacePath(binding.Workspace) != workspace {
		return "", fmt.Errorf("user-workspace: workspace binding mismatch for session %q", sessionKey)
	}
	return workspace, nil
}
