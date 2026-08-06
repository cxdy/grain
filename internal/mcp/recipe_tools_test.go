package mcp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/client"
	grainmcp "github.com/cxdy/grain/internal/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRecipeListAddCreateTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)

	src := filepath.Join(t.TempDir(), "lab.yaml")
	body := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: lab
spec:
  image: grain-ubuntu
  cpus: 2
`)
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}

	vms := map[string]*client.Instance{}
	sess, _ := sessionWithMock(t, vms)
	ctx := t.Context()

	// list empty
	st, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: grainmcp.ToolRecipeList, Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	text := textOf(t, st)
	if !strings.Contains(text, "count") {
		t.Fatalf("list: %s", text)
	}

	// add file — must not create VM
	st, err = sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolRecipeAdd,
		Arguments: map[string]any{
			"source": src,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	text = textOf(t, st)
	if !strings.Contains(text, "created_vm") || strings.Contains(text, `"created_vm":true`) {
		t.Fatalf("add must not create vm: %s", text)
	}
	if len(vms) != 0 {
		t.Fatalf("mock vms after add: %d", len(vms))
	}

	st, err = sess.CallTool(ctx, &mcp.CallToolParams{Name: grainmcp.ToolRecipeList, Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, st), "lab") {
		t.Fatalf("list after add: %s", textOf(t, st))
	}

	// create from recipe
	st, err = sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolRecipeCreate,
		Arguments: map[string]any{
			"recipe": "lab",
			"name":   "lab-1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, st), "lab-1") {
		t.Fatalf("create: %s", textOf(t, st))
	}
}
