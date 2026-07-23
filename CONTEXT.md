# CC-Connect Messaging

This context defines the core language for routing messages between users, coding agents, and external automation.

## Language

**Reply Route**:
A stable platform destination for proactive messages, independent of whether an Agent Session is active. It is represented internally by a session key such as `wecom:{chatID}:{userID}`.
_Avoid_: Codex session, active session, callback session

**Agent Session**:
A temporary running conversation with a coding agent. Ending it does not invalidate the Reply Route for the originating platform conversation.
_Avoid_: Reply route, enterprise chat session, `CC_SESSION`

**Shared Workspace**:
A working directory available for multiple users to select and work in. Sharing a workspace does not imply sharing an Agent Session.
_Avoid_: Shared session, group session

**User Workspace**:
A working directory reserved for one user. Selecting it does not affect another user's Workspace Selection.
_Avoid_: User session, home directory

**Workspace Selection**:
A user's current choice between their User Workspace and an available Shared Workspace across their platform conversations. Another user's selection remains unchanged.
_Avoid_: Channel workspace, global workspace binding

**Controller Agent**:
The coding agent that receives the user's request through CC-Connect and delegates execution to another service.
_Avoid_: Server Codex, source Codex, chat Codex

**Execution Agent**:
The coding agent that receives a delegated command in the Jenkins-side environment and starts the requested Jenkins work.
_Avoid_: Jenkins Codex, remote Codex, target Codex
