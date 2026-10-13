package script

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/hakastein/cliche"
	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/diag"
)

// The login is looked up only when a script first needs it, so a script that fails before that reports its own fault.
type Host struct {
	Call    func(ctx context.Context, call func(context.Context, *youtrack.Client) (*youtrack.Node, error)) (*youtrack.Node, *diag.Fault)
	Address func() (string, *diag.Fault)
	Warn    func(*youtrack.Warning)
}

// run is the state of one call of a script: what the API reaches and whether it may have written.
type run struct {
	host Host
	// wrote holds once the instance may have changed, even when a fault came after.
	wrote bool
}

const apiName = "ytrack/v1"

// Run runs a script command and returns the mapping it printed and whether the instance may have changed.
func Run(ctx context.Context, command *cliche.Command, in cliche.Input, host Host) (answer *youtrack.Node, wrote bool,
	fault *diag.Fault,
) {
	r := &run{host: host}
	env := cliche.Env{
		Modules: map[string]any{apiName: r.v1()},
		Node:    cliche.NodeFS | cliche.NodeFetch | cliche.NodeBuffer,
		// A script that catches a failed fetch reads the code of ytrack in it.
		Error: func(err error) error { return thrown{fault: FaultOf(err)} },
	}
	returned, err := cliche.Run(ctx, command, in, env)
	if err == nil {
		answer, fault = document(command, returned)
	} else if fault = FaultOf(err); fault == nil {
		fault = diag.FromError(err)
	}
	if fault != nil && fault.Code == diag.CodeScriptFailed && command.Builtin() {
		defect(fault)
	}
	return answer, r.wrote, fault
}

func document(command *cliche.Command, returned any) (*youtrack.Node, *diag.Fault) {
	node, err := toNode(returned)
	if err == nil && node.Kind() != youtrack.MapNode {
		err = errors.New("is no object, and a command prints one YAML mapping")
	}
	if err != nil {
		return nil, FaultOf(&cliche.ScriptError{Message: "the value exports.command returned " + err.Error(),
			File: command.File()})
	}
	return node, nil
}

// FaultOf is the document of an error a script command ends in; nil for an error that is not of a script.
func FaultOf(err error) *diag.Fault {
	var fault *diag.Fault
	var script *cliche.ScriptError
	var usage *cliche.UsageError
	var file *cliche.FileError
	var fetch *cliche.FetchError
	var interrupted *cliche.InterruptedError
	switch {
	case errors.As(err, &fault):
		return fault
	case errors.As(err, &script):
		return scriptFault(script.Message, script.File, script.Line, script.Column)
	case errors.As(err, &usage):
		return &diag.Fault{Code: youtrack.CodeBadUsage, Message: usage.Message}
	case errors.As(err, &file):
		return &diag.Fault{Code: youtrack.CodeBadUsage, Message: file.Message,
			Details: []youtrack.Pair{{Key: "path", Value: youtrack.NewString(file.Path)}}}
	case errors.As(err, &fetch):
		return &diag.Fault{Code: youtrack.CodeUpstreamFailed, Message: fetch.Error(),
			Details: []youtrack.Pair{{Key: "url", Value: youtrack.NewString(fetch.URL)}}}
	case errors.As(err, &interrupted):
		return &diag.Fault{Code: youtrack.CodeUpstreamFailed, Message: interrupted.Error()}
	}
	return nil
}

func scriptFault(message, file string, line, column int) *diag.Fault {
	fault := &diag.Fault{Code: diag.CodeScriptFailed, Message: message}
	if file != "" {
		fault.Details = append(fault.Details, youtrack.Pair{Key: "file", Value: youtrack.NewString(file)})
	}
	if line > 0 {
		fault.Details = append(fault.Details,
			youtrack.Pair{Key: "line", Value: youtrack.NewNumber(json.Number(strconv.Itoa(line)))},
			youtrack.Pair{Key: "column", Value: youtrack.NewNumber(json.Number(strconv.Itoa(column)))})
	}
	return fault
}

// callerFault is a script_failed at the line of the script that called the API.
func callerFault(ctx context.Context, message string) error {
	file, line, column := cliche.Place(ctx)
	return thrown{fault: scriptFault(message, file, line, column)}
}

// thrown is a fault as a script catches it: code, message, details and wrote.
type thrown struct {
	fault *diag.Fault
}

func (t thrown) Error() string {
	return t.fault.Error()
}

func (t thrown) Unwrap() error {
	return t.fault
}

func (t thrown) ScriptFields() cliche.Map {
	return cliche.NewObject([]string{"code", "message", "details", "wrote"}, []any{string(t.fault.Code),
		t.fault.Message, valueOf(youtrack.NewMap(t.fault.Details...)), t.fault.MayHaveWritten()})
}

// A builtin script is code of ytrack, so its defect is a bug of ytrack and panics as one in Go would.
func defect(fault *diag.Fault) {
	place := make([]string, 0, len(fault.Details))
	for _, pair := range fault.Details {
		place = append(place, pair.Value.Value())
	}
	panic("a builtin script failed at " + strings.Join(place, ":") + ": " + fault.Message)
}
