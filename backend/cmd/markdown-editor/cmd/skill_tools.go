package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/skills"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerSkillTools(s *server.MCPServer, ctx *MCPContext) {
	s.AddTool(
		mcp.NewTool("list_skills",
			mcp.WithDescription("List all user-scope Claude Code skills with name, description, and enabled state. Reads ~/.claude/skills/."),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			summaries, err := listSkills(ctx)
			if err != nil {
				return mcpTextResult(fmt.Sprintf("Error: %v", err)), nil
			}
			out, _ := json.MarshalIndent(summaries, "", "  ")
			return mcpTextResult(string(out)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("read_skill",
			mcp.WithDescription("Read a skill's SKILL.md file (frontmatter + body) and list its supporting files."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Skill name (directory name in ~/.claude/skills/)")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name := req.GetString("name", "")
			if name == "" {
				return mcpTextResult("Error: name is required"), nil
			}
			return readSkill(ctx, name), nil
		},
	)

	s.AddTool(
		mcp.NewTool("read_skill_file",
			mcp.WithDescription("Read a supporting file inside a skill's directory (e.g. references/foo.md)."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Skill name")),
			mcp.WithString("file", mcp.Required(), mcp.Description("Relative path inside the skill dir, e.g. 'references/schemas.md'")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name := req.GetString("name", "")
			file := req.GetString("file", "")
			if name == "" || file == "" {
				return mcpTextResult("Error: name and file are required"), nil
			}
			return readSkillFile(ctx, name, file), nil
		},
	)

	s.AddTool(
		mcp.NewTool("create_skill",
			mcp.WithDescription("Create a new user-scope skill. Frontmatter is built from explicit arguments; the body is the markdown content Claude follows when the skill runs. Strict validation: name must be lowercase letters/digits/hyphens (max 64), description required."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Skill name (becomes directory and /slash-command)")),
			mcp.WithString("description", mcp.Required(), mcp.Description("What the skill does and when to use it")),
			mcp.WithString("content", mcp.Required(), mcp.Description("Markdown body of SKILL.md (without frontmatter)")),
			mcp.WithString("allowed_tools", mcp.Description("Space-separated tools Claude can use without asking (e.g. 'Read Grep')")),
			mcp.WithBoolean("disable_model_invocation", mcp.Description("If true, only the user can invoke via /name (Claude cannot trigger automatically)")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name := req.GetString("name", "")
			desc := req.GetString("description", "")
			body := req.GetString("content", "")
			allowed := req.GetString("allowed_tools", "")
			disableModel := req.GetString("disable_model_invocation", "") == "true"
			if name == "" || desc == "" || body == "" {
				return mcpTextResult("Error: name, description, and content are required"), nil
			}
			return createSkill(ctx, name, desc, body, allowed, disableModel), nil
		},
	)

	s.AddTool(
		mcp.NewTool("update_skill",
			mcp.WithDescription("Update an existing skill's SKILL.md. Provide any subset of description, content, allowed_tools. Strict validation."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Skill name")),
			mcp.WithString("description", mcp.Description("New description")),
			mcp.WithString("content", mcp.Description("New body (full replacement)")),
			mcp.WithString("allowed_tools", mcp.Description("Space-separated tools list")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name := req.GetString("name", "")
			if name == "" {
				return mcpTextResult("Error: name is required"), nil
			}
			return updateSkill(ctx, name,
				req.GetString("description", ""),
				req.GetString("content", ""),
				req.GetString("allowed_tools", ""),
			), nil
		},
	)

	s.AddTool(
		mcp.NewTool("enable_skill",
			mcp.WithDescription("Re-enable a previously disabled skill (writes skillOverrides['name']='on' to settings)."),
			mcp.WithString("name", mcp.Required()),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return setSkillState(ctx, req.GetString("name", ""), skills.OverrideOn), nil
		},
	)

	s.AddTool(
		mcp.NewTool("disable_skill",
			mcp.WithDescription("Disable a skill via settings.json skillOverrides without deleting it."),
			mcp.WithString("name", mcp.Required()),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return setSkillState(ctx, req.GetString("name", ""), skills.OverrideOff), nil
		},
	)

	s.AddTool(
		mcp.NewTool("delete_skill",
			mcp.WithDescription("Delete a skill directory and all its files. Requires confirm=true."),
			mcp.WithString("name", mcp.Required()),
			mcp.WithBoolean("confirm", mcp.Description("Must be true to actually delete")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name := req.GetString("name", "")
			if req.GetString("confirm", "") != "true" {
				return mcpTextResult("Refusing to delete: set confirm=true"), nil
			}
			tctx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()
			if err := ctx.skills.DeleteSkill(tctx, name); err != nil {
				return mcpTextResult(fmt.Sprintf("Error: %v", err)), nil
			}
			_ = ctx.skillSettings.SetState(name, skills.OverrideOn)
			return mcpTextResult(fmt.Sprintf("Deleted skill %q", name)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("validate_skill",
			mcp.WithDescription("Validate a skill's frontmatter strictly and report issues."),
			mcp.WithString("name", mcp.Required()),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return validateSkill(ctx, req.GetString("name", "")), nil
		},
	)
}

// ---- helpers ----

func mcpTextResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent(s)}}
}

type skillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	State       string `json:"state"`
	Valid       bool   `json:"valid"`
}

func listSkills(ctx *MCPContext) ([]skillSummary, error) {
	root := ctx.skills.Root()
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	overrides, _ := ctx.skillSettings.GetAll()
	var out []skillSummary
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		data, err := os.ReadFile(filepath.Join(root, name, skills.MainFile))
		if err != nil {
			continue
		}
		fm, _, err := skills.Parse(data)
		valid := err == nil
		state := overrides[name]
		if state == "" {
			state = skills.OverrideOn
		}
		out = append(out, skillSummary{
			Name:        name,
			Description: fm.String("description"),
			Enabled:     state != skills.OverrideOff,
			State:       string(state),
			Valid:       valid,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func readSkill(ctx *MCPContext, name string) *mcp.CallToolResult {
	root := ctx.skills.Root()
	skillDir := filepath.Join(root, name)
	mainPath := filepath.Join(skillDir, skills.MainFile)
	data, err := os.ReadFile(mainPath)
	if err != nil {
		return mcpTextResult(fmt.Sprintf("Error: %v", err))
	}
	fm, body, err := skills.Parse(data)
	if err != nil {
		return mcpTextResult(fmt.Sprintf("Parse error: %v\n\nRaw content:\n%s", err, data))
	}

	var files []string
	_ = filepath.WalkDir(skillDir, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(skillDir, p)
		if rel == skills.MainFile {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})

	out := map[string]any{
		"name":             name,
		"frontmatter":      fm,
		"content":          body,
		"supporting_files": files,
	}
	js, _ := json.MarshalIndent(out, "", "  ")
	return mcpTextResult(string(js))
}

func readSkillFile(ctx *MCPContext, name, file string) *mcp.CallToolResult {
	clean := filepath.Clean(file)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return mcpTextResult("Error: file must be a relative path inside the skill dir")
	}
	abs := filepath.Join(ctx.skills.Root(), name, clean)
	data, err := os.ReadFile(abs)
	if err != nil {
		return mcpTextResult(fmt.Sprintf("Error: %v", err))
	}
	return mcpTextResult(string(data))
}

func createSkill(ctx *MCPContext, name, desc, body, allowed string, disableModel bool) *mcp.CallToolResult {
	fm := skills.Frontmatter{
		"name":        name,
		"description": desc,
	}
	if allowed != "" {
		fm["allowed-tools"] = allowed
	}
	if disableModel {
		fm["disable-model-invocation"] = true
	}
	if issues := skills.ValidateStrict(fm, name); len(issues) > 0 {
		var lines []string
		for _, i := range issues {
			lines = append(lines, "- "+i.String())
		}
		return mcpTextResult("Validation failed:\n" + strings.Join(lines, "\n"))
	}
	tctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ctx.skills.CreateSkill(tctx, name, fm, body); err != nil {
		return mcpTextResult(fmt.Sprintf("Error: %v", err))
	}
	return mcpTextResult(fmt.Sprintf("Created skill %q at ~/.claude/skills/%s/", name, name))
}

func updateSkill(ctx *MCPContext, name, desc, body, allowed string) *mcp.CallToolResult {
	mainPath := filepath.Join(ctx.skills.Root(), name, skills.MainFile)
	data, err := os.ReadFile(mainPath)
	if err != nil {
		return mcpTextResult(fmt.Sprintf("Error: %v", err))
	}
	fm, oldBody, err := skills.Parse(data)
	if err != nil {
		return mcpTextResult(fmt.Sprintf("Parse error: %v", err))
	}
	if fm == nil {
		fm = skills.Frontmatter{"name": name}
	}
	if desc != "" {
		fm["description"] = desc
	}
	if allowed != "" {
		fm["allowed-tools"] = allowed
	}
	newBody := oldBody
	if body != "" {
		newBody = body
	}
	if issues := skills.ValidateStrict(fm, name); len(issues) > 0 {
		var lines []string
		for _, i := range issues {
			lines = append(lines, "- "+i.String())
		}
		return mcpTextResult("Validation failed:\n" + strings.Join(lines, "\n"))
	}
	out, err := skills.Marshal(fm, newBody)
	if err != nil {
		return mcpTextResult(fmt.Sprintf("Marshal error: %v", err))
	}
	if err := os.WriteFile(mainPath, out, 0o644); err != nil {
		return mcpTextResult(fmt.Sprintf("Write error: %v", err))
	}
	tctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = ctx.skills.ImportAll(tctx)
	return mcpTextResult(fmt.Sprintf("Updated skill %q", name))
}

func setSkillState(ctx *MCPContext, name string, state skills.SkillOverrideState) *mcp.CallToolResult {
	if name == "" {
		return mcpTextResult("Error: name is required")
	}
	if err := ctx.skillSettings.SetState(name, state); err != nil {
		return mcpTextResult(fmt.Sprintf("Error: %v", err))
	}
	return mcpTextResult(fmt.Sprintf("Skill %q state set to %q", name, state))
}

func validateSkill(ctx *MCPContext, name string) *mcp.CallToolResult {
	mainPath := filepath.Join(ctx.skills.Root(), name, skills.MainFile)
	data, err := os.ReadFile(mainPath)
	if err != nil {
		return mcpTextResult(fmt.Sprintf("Error: %v", err))
	}
	fm, _, err := skills.Parse(data)
	if err != nil {
		return mcpTextResult(fmt.Sprintf("Parse error: %v", err))
	}
	issues := skills.ValidateStrict(fm, name)
	if len(issues) == 0 {
		return mcpTextResult("OK: no issues")
	}
	var lines []string
	for _, i := range issues {
		lines = append(lines, "- "+i.String())
	}
	return mcpTextResult(fmt.Sprintf("Found %d issues:\n%s", len(issues), strings.Join(lines, "\n")))
}
