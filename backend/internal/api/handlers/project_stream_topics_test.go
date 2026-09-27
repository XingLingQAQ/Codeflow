package handlers

// FU for the project stream's topic allowlist (T1.12.b, group 2): the route
// admits the project's own flow topic and — when the project has a bound
// workspace root that resolves — its own workspace topic, derived exactly the
// way CreateWorkspaceWatch derives the topic it hands the client. Anything
// else, including another project's workspace topic, is absent from the set.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codeflow/backend/internal/project"
	"github.com/codeflow/backend/internal/workspace"
)

// withWorkspaceService installs a fresh unrestricted workspace service for the
// duration of one test and restores the previous one (or the unset state).
func withWorkspaceService(t *testing.T) *workspace.FSService {
	t.Helper()
	hadSvc := workspace.HasService()
	var prev workspace.Service
	if hadSvc {
		prev = workspace.GetService()
	}
	svc := workspace.NewFSService(nil)
	workspace.SetService(svc)
	t.Cleanup(func() {
		if hadSvc {
			workspace.SetService(prev)
		} else {
			workspace.SetService(nil)
		}
	})
	return svc
}

func topicSetOf(topics []string) map[string]bool {
	set := make(map[string]bool, len(topics))
	for _, topic := range topics {
		set[topic] = true
	}
	return set
}

// TestProjectStreamTopicsIncludesOwnWorkspaceRoot: a project whose root resolves
// gets exactly its flow topic plus its own workspace topic — the same string the
// watch API returns for that root.
func TestProjectStreamTopicsIncludesOwnWorkspaceRoot(t *testing.T) {
	withWorkspaceService(t)

	root := t.TempDir()
	p := &project.Project{ID: "proj-a", WorkspaceRoot: root}

	topics := projectStreamTopics(p)
	if len(topics) != 2 {
		t.Fatalf("projectStreamTopics(%q) = %v, want the flow topic and one workspace topic", root, topics)
	}
	if topics[0] != "flow:project:proj-a" {
		t.Fatalf("first topic = %q, want flow:project:proj-a", topics[0])
	}

	// The topic the client is handed is derived through Resolve(root, ".") — the
	// normalization CreateWorkspaceWatch uses — so the two must agree byte for
	// byte or the subscription would never match the broadcast.
	absRoot, err := workspace.GetService().Resolve(root, ".")
	if err != nil {
		t.Fatalf("Resolve(%q, \".\") failed: %v", root, err)
	}
	want := workspace.WorkspaceTopicForRoot(absRoot)
	if topics[1] != want {
		t.Fatalf("workspace topic = %q, want %q", topics[1], want)
	}
	if !topicSetOf(topics)[want] {
		t.Fatalf("workspace topic %q missing from %v", want, topics)
	}
}

// TestProjectStreamTopicsNormalizesRootSpelling: the allowlist is built from the
// resolved root, so a project that stores a spelling of the same directory
// (here: a trailing separator) still admits the topic of the resolved one.
func TestProjectStreamTopicsNormalizesRootSpelling(t *testing.T) {
	withWorkspaceService(t)

	root := t.TempDir()
	absRoot, err := workspace.GetService().Resolve(root, ".")
	if err != nil {
		t.Fatalf("Resolve(%q, \".\") failed: %v", root, err)
	}
	want := workspace.WorkspaceTopicForRoot(absRoot)

	p := &project.Project{ID: "proj-spelling", WorkspaceRoot: root + string(filepath.Separator)}
	if !topicSetOf(projectStreamTopics(p))[want] {
		t.Fatalf("topics for %q = %v, want to contain %q", p.WorkspaceRoot, projectStreamTopics(p), want)
	}
}

// TestProjectStreamTopicsWithoutRootOrUnresolvableRoot: no root, an unbound
// project, and a root that does not exist all fall back to the flow topic alone
// (and never to a topic built from the unresolved spelling).
func TestProjectStreamTopicsWithoutRootOrUnresolvableRoot(t *testing.T) {
	withWorkspaceService(t)

	missing := filepath.Join(t.TempDir(), "does-not-exist")
	cases := []struct {
		name string
		p    *project.Project
	}{
		{name: "no root", p: &project.Project{ID: "proj-none"}},
		{name: "blank root", p: &project.Project{ID: "proj-blank", WorkspaceRoot: "   "}},
		{name: "unbound", p: &project.Project{ID: "proj-unbound", BindingState: project.BindingStateUnbound}},
		{name: "missing directory", p: &project.Project{ID: "proj-missing", WorkspaceRoot: missing}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			topics := projectStreamTopics(tc.p)
			want := []string{"flow:project:" + tc.p.ID}
			if len(topics) != len(want) || topics[0] != want[0] {
				t.Fatalf("projectStreamTopics(%s) = %v, want %v", tc.name, topics, want)
			}
			// The unresolved spelling must not have been hashed into a topic.
			if topicSetOf(topics)[workspace.WorkspaceTopicForRoot(tc.p.WorkspaceRoot)] && tc.p.WorkspaceRoot != "" {
				t.Fatalf("topics for %s = %v contain a topic built from the unresolved root", tc.name, topics)
			}
		})
	}
}

// TestProjectStreamTopicsExcludesOtherProjectsWorkspace: two projects with two
// different roots share no workspace topic.
func TestProjectStreamTopicsExcludesOtherProjectsWorkspace(t *testing.T) {
	withWorkspaceService(t)

	rootA := t.TempDir()
	rootB := t.TempDir()
	absB, err := workspace.GetService().Resolve(rootB, ".")
	if err != nil {
		t.Fatalf("Resolve(%q, \".\") failed: %v", rootB, err)
	}
	topicB := workspace.WorkspaceTopicForRoot(absB)

	a := &project.Project{ID: "proj-a", WorkspaceRoot: rootA}
	topicsA := projectStreamTopics(a)
	if topicSetOf(topicsA)[topicB] {
		t.Fatalf("project A stream admits project B's workspace topic %q: %v", topicB, topicsA)
	}
	if topicSetOf(topicsA)["flow:project:proj-b"] {
		t.Fatalf("project A stream admits another project's flow topic: %v", topicsA)
	}
	if !topicSetOf(topicsA)["flow:project:proj-a"] {
		t.Fatalf("project A stream does not admit its own flow topic: %v", topicsA)
	}

	b := &project.Project{ID: "proj-b", WorkspaceRoot: rootB}
	if !topicSetOf(projectStreamTopics(b))[topicB] {
		t.Fatalf("project B stream does not admit its own workspace topic %q", topicB)
	}
}

// TestProjectStreamTopicsWithoutWorkspaceService: with no workspace service
// installed the allowlist is the flow topic — the workspace topic cannot be
// derived, so it must not be guessed.
func TestProjectStreamTopicsWithoutWorkspaceService(t *testing.T) {
	hadSvc := workspace.HasService()
	var prev workspace.Service
	if hadSvc {
		prev = workspace.GetService()
	}
	workspace.SetService(nil)
	t.Cleanup(func() {
		if hadSvc {
			workspace.SetService(prev)
		} else {
			workspace.SetService(nil)
		}
	})

	root := t.TempDir()
	topics := projectStreamTopics(&project.Project{ID: "proj-nosvc", WorkspaceRoot: root})
	if len(topics) != 1 || topics[0] != "flow:project:proj-nosvc" {
		t.Fatalf("projectStreamTopics without a workspace service = %v, want only the flow topic", topics)
	}
}

// TestProjectStreamTopicsResolveFailureAddsNoTopic: a root that resolves to a
// file (not a directory) is refused by Resolve, and the allowlist stays at the
// flow topic rather than admitting a topic nothing can broadcast on.
func TestProjectStreamTopicsResolveFailureAddsNoTopic(t *testing.T) {
	withWorkspaceService(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	topics := projectStreamTopics(&project.Project{ID: "proj-file", WorkspaceRoot: file})
	if len(topics) != 1 || topics[0] != "flow:project:proj-file" {
		t.Fatalf("projectStreamTopics(file root) = %v, want only the flow topic", topics)
	}
}
