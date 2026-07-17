package core

import (
	"encoding/hex"
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

func ensureUserWorkspaceDir(baseDir, userID string) (string, error) {
	baseInfo, err := os.Lstat(baseDir)
	if err != nil {
		return "", fmt.Errorf("user-workspace: inspect base_dir: %w", err)
	}
	if !baseInfo.IsDir() || baseInfo.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("user-workspace: base_dir must be a real directory")
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
}

func (e *Engine) prepareUserWorkspace(msg *Message) (string, error) {
	if msg == nil || msg.Platform != "wecom" {
		return "", fmt.Errorf("user-workspace: authenticated WeCom message required")
	}
	workspace, err := ensureUserWorkspaceDir(e.baseDir, msg.UserID)
	if err != nil {
		return "", err
	}
	projectKey := "project:" + e.name
	channelKey := workspaceChannelKey(msg.Platform, msg.UserID)
	if binding := e.workspaceBindings.ListByProject(projectKey)[channelKey]; binding == nil || normalizeWorkspacePath(binding.Workspace) != workspace {
		e.workspaceBindings.Bind(projectKey, channelKey, msg.UserID, workspace)
	}
	return workspace, nil
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
	workspace, err := ensureUserWorkspaceDir(e.baseDir, userID)
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
