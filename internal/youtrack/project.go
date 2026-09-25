package youtrack

import (
	"context"
	"net/http"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

// A token without project-read is sent neither archived nor leader, and a field asked for that did not arrive is
// refused, so a default holds only fields every reader of a project is sent.
const (
	// The types of work time is written against are settings of the project's time tracking, not custom fields
	// of it, and they are printed with enabled because a project that has time tracking off still carries a set.
	ProjectShowFields = "shortName,name,plugins(timeTrackingSettings(enabled,workItemTypes(name)))"
	ProjectListFields = "shortName,name"
)

// A Call is a command whose words passed every check that needs no server, waiting for a client to send it.
type Call func(ctx context.Context, c *Client) (*render.Node, *diag.Fault)

// ShowProject is the call for the fields of expression, or for them added to ProjectShowFields when it starts with +.
func ShowProject(code, expression string) (Call, *diag.Fault) {
	code, fault := parseProjectCode(code)
	if fault != nil {
		return nil, fault
	}
	requested, fault := parseFields(expression, ProjectShowFields)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.showProject(ctx, spec, code, requested)
	}, nil
}

// ListProjects is the call for one page of the projects with the fields of expression, or with them added to
// ProjectListFields when it starts with +.
func ListProjects(expression string, page Page) (Call, *diag.Fault) {
	if fault := page.parse(); fault != nil {
		return nil, fault
	}
	requested, fault := parseFields(expression, ProjectListFields)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.listProjects(ctx, spec, requested, page)
	}, nil
}

func (c *Client) showProject(ctx context.Context, spec *schemas, code string, requested []requestedField) (*render.Node, *diag.Fault) {
	projects, fault := c.read(ctx, spec, "Project", requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetProject(ctx, code, fields)
	})
	if fault != nil {
		return nil, fault
	}
	return projects[0], nil
}

func (c *Client) listProjects(ctx context.Context, spec *schemas, requested []requestedField, page Page) (*render.Node, *diag.Fault) {
	return c.listPage(ctx, spec, "projects", "[]Project", requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return c.apiGetProjects(ctx, fields, w)
	})
}
