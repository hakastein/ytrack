package youtrack

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

const UserShowFields = "login,fullName,email,banned"

const UserListFields = "login,fullName,banned"

func ListUsers(search, expression string, page Page) (Call, *diag.Fault) {
	if fault := page.parse(); fault != nil {
		return nil, fault
	}
	requested, fault := parseFields(expression, UserListFields)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.listUsers(ctx, spec, search, requested, page)
	}, nil
}

func (c *Client) listUsers(ctx context.Context, spec *schemas, search string, requested []requestedField, page Page) (*render.Node, *diag.Fault) {
	return c.listPage(ctx, spec, "users", "[]User", requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return c.apiGetUsers(ctx, search, fields, w)
	})
}

func ShowUser(login, expression string) (Call, *diag.Fault) {
	login, fault := parseLogin(login)
	if fault != nil {
		return nil, fault
	}
	requested, fault := parseFields(expression, UserShowFields)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.showUser(ctx, spec, login, requested)
	}, nil
}

func (c *Client) showUser(ctx context.Context, spec *schemas, login string, requested []requestedField) (*render.Node, *diag.Fault) {
	users, fault := c.read(ctx, spec, "User", requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetUser(ctx, login, fields)
	})
	if fault != nil {
		return nil, noSuchLogin(login, fault)
	}
	return users[0], nil
}

func noSuchLogin(login string, fault *diag.Fault) *diag.Fault {
	if fault.Code != diag.NotFound {
		return fault
	}
	message := fmt.Sprintf("the server has no user of the login %s, and %s", render.Quote(login), findByName(login))
	return &diag.Fault{Code: fault.Code, Message: message, Details: fault.Details}
}

func findByName(arg string) string {
	quoted := "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	return render.Quote("ytrack user list --query "+quoted) + " finds one by login or name"
}

func CurrentUser(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
	users, fault := c.read(ctx, loadSchemas(), "Me", []requestedField{{name: loginKey}, {name: "fullName"}}, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetCurrentUser(ctx, fields)
	})
	if fault != nil {
		return nil, fault
	}
	return users[0], nil
}
