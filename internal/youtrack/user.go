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

// A ban is no reason a user is left out of a search or out of the values a field allows, so it is told either way.
const UserListFields = "login,fullName,banned"

// ListUsers is the call for one page of the users the server finds for search, with the fields of expression, or
// with them added to UserListFields when it starts with +.
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

// ShowUser is the call for the user of that login with the fields of expression, or with them added to
// UserShowFields when it starts with +.
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

// The status display names the status and nothing of the call, while a 404 here is the one answer that tells the
// caller what they addressed the user by. Only the text is the command's own: the code and the upstream_ keys the
// server filled are carried over, and the fault the pass built is left as it was.
func noSuchLogin(login string, fault *diag.Fault) *diag.Fault {
	if fault.Code != diag.NotFound {
		return fault
	}
	message := fmt.Sprintf("the server has no user of the login %s, and %s", render.Quote(login), findByName(login))
	return &diag.Fault{Code: fault.Code, Message: message, Details: fault.Details}
}

// The way from a name to a login. user list takes the text as the value of a flag, so a name holding a space or
// a leading dash reaches it whole; it stands in single quotes, where a shell takes every byte as it is but the
// quote itself.
func findByName(arg string) string {
	quoted := "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	return render.Quote("ytrack user list --query "+quoted) + " finds one by login or name"
}

// CurrentUser is the call for the user the server takes the token for: login is a user's identity and fullName is for
// a human, and no rights hide either of them from the user they belong to.
func CurrentUser(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
	users, fault := c.read(ctx, loadSchemas(), "Me", []requestedField{{name: loginKey}, {name: "fullName"}}, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetCurrentUser(ctx, fields)
	})
	if fault != nil {
		return nil, fault
	}
	return users[0], nil
}
