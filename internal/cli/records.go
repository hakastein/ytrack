package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

// ytrack keeps a directory of its own under the home directory: the login records in one file, and room for a
// cache beside them. Both hold what a token reaches, so the directory is 0700 and the file 0600.
const (
	recordsDirectory = ".ytrack"
	recordsName      = "auth.json"
	cacheName        = "cache"

	recordsDirectoryMode fs.FileMode = 0o700
	recordsFileMode      fs.FileMode = 0o600
)

// A record is one login: a token, the address it belongs to and what it applies to.
type record struct {
	scope   scope
	address *url.URL
	token   string
	// The bytes the file held this record as, written back untouched; a record that did not come from a file has
	// to be given them before it is saved.
	raw json.RawMessage
}

// A scope is what a login record applies to, and these are the two states there are: everywhere, or one directory
// and everything under it.
type scope struct {
	// An absolute path as filepath.Clean spells it, or "" for the record that applies everywhere; no directory is
	// spelled that way, so the two never stand for one another.
	directory string
}

// everywhere is the scope of the record a file holds at most one of: the one written with no scope.
func everywhere() scope {
	return scope{}
}

func inDirectory(directory string) scope {
	return scope{directory: directory}
}

func (s scope) isEverywhere() bool {
	return s.directory == ""
}

// spelled is how a document and an origin name the scope. No directory is spelled "global", so the word cannot be
// read back as one.
func (s scope) spelled() string {
	if s.isEverywhere() {
		return "global"
	}
	return s.directory
}

// covers is whether the scope is dir or an ancestor of it by path components: /a/b holds in /a/b/c, not in /a/bc.
func (s scope) covers(dir string) bool {
	separator := string(filepath.Separator)
	return dir == s.directory || strings.HasPrefix(dir, strings.TrimSuffix(s.directory, separator)+separator)
}

// The file as JSON holds it. A record without scope, or with a null one, is the record for everywhere.
type recordJSON struct {
	Scope *string `json:"scope,omitempty"`
	URL   string  `json:"url"`
	Token string  `json:"token"`
}

// recordsPath is the file of login records under the home directory env names, or "" when env names none. A
// relative HOME is no home directory: which file it named would then depend on where the process was started.
func recordsPath(home string) string {
	if home == "" || !filepath.IsAbs(home) {
		return ""
	}
	return filepath.Join(home, recordsDirectory, recordsName)
}

// cacheDirectory is where the metadata of projects is kept between calls, beside the login records, or "" where
// env names no home directory to keep it in and there is to be no cache.
func cacheDirectory(home string) string {
	if home == "" || !filepath.IsAbs(home) {
		return ""
	}
	return filepath.Join(home, recordsDirectory, cacheName)
}

// homeReason is why recordsPath named no file.
func homeReason(home string) string {
	if home == "" {
		return homeVariable + " is not set"
	}
	return fmt.Sprintf("%s %s is not an absolute path", homeVariable, render.Quote(home))
}

// readRecords is the whole file or a refusal naming it. ytrack alone writes the file, so anything in it other than
// the records it writes is damage, told as that and nothing finer: which record, which key and what JSON says about
// them is the layout of the file, and no caller is meant to open it.
func readRecords(path string) ([]record, *diag.Fault) {
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, barred(path, "the saved logins cannot be read", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	// A pointer tells null from an array of none: encoding/json puts null into a slice without a word.
	var raw *[]json.RawMessage
	if err := decoder.Decode(&raw); err != nil || raw == nil {
		return nil, damaged(path)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, damaged(path)
	}
	records := make([]record, 0, len(*raw))
	seen := make(map[scope]bool, len(*raw))
	for _, item := range *raw {
		read, ok := readRecord(item)
		if !ok || seen[read.scope] {
			return nil, damaged(path)
		}
		seen[read.scope] = true
		records = append(records, read)
	}
	return records, nil
}

func readRecord(item json.RawMessage) (record, bool) {
	decoder := json.NewDecoder(bytes.NewReader(item))
	decoder.DisallowUnknownFields()
	var held recordJSON
	if err := decoder.Decode(&held); err != nil || held.URL == "" || held.Token == "" {
		return record{}, false
	}
	address, reason := usableAddress(held.URL, "")
	if reason != "" || usableToken(held.Token, "") != "" {
		return record{}, false
	}
	read := record{address: address, token: held.Token, raw: item}
	if held.Scope == nil {
		return read, true
	}
	// A scope is matched against the working directory as it stands, so a path written any other way than the
	// kernel resolves it names a directory no call can be in.
	directory := *held.Scope
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return record{}, false
	}
	read.scope = inDirectory(directory)
	return read, true
}

// saveRecords puts records where path held its own, or takes the file away when none are left. Each record goes
// back as the bytes it was read as, so a login this call did not touch keeps the spelling, the escapes and the
// order of keys its writer gave it; the records themselves are ordered here, so the same logins make the same file.
func saveRecords(path string, records []record) *diag.Fault {
	if len(records) == 0 {
		// The directory stays: it is also where the metadata of projects is cached.
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return barred(path, "the saved logins cannot be removed", err)
		}
		return nil
	}
	ordered := slices.Clone(records)
	// The record for everywhere has no directory of its own, and no absolute path sorts before none of one.
	slices.SortFunc(ordered, func(a, b record) int { return strings.Compare(a.scope.directory, b.scope.directory) })
	var content bytes.Buffer
	content.WriteString("[\n")
	for i, held := range ordered {
		content.WriteString("  ")
		content.Write(held.raw)
		if i < len(ordered)-1 {
			content.WriteByte(',')
		}
		content.WriteByte('\n')
	}
	content.WriteString("]\n")
	if err := replaceFile(path, content.Bytes()); err != nil {
		return barred(path, "the saved logins cannot be written", err)
	}
	return nil
}

// The file is written beside itself and renamed over, so a call that fails halfway leaves the logins as they were
// rather than half of them. os.CreateTemp opens at 0600 and Chmod holds it there whatever the umask.
func replaceFile(path string, content []byte) (err error) {
	directory := filepath.Dir(path)
	if err = os.MkdirAll(directory, recordsDirectoryMode); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".auth-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = temporary.Close()
			_ = os.Remove(temporary.Name())
		}
	}()
	if _, err = temporary.Write(content); err != nil {
		return err
	}
	if err = temporary.Chmod(recordsFileMode); err != nil {
		return err
	}
	if err = temporary.Sync(); err != nil {
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}

// takeOut is the record held for exactly this place and the records without it. An ancestor's record is not it:
// logging out in a subdirectory would take away the login of the directory above.
func takeOut(records []record, wanted scope) (record, []record, bool) {
	for i, held := range records {
		if held.scope == wanted {
			return held, slices.Delete(slices.Clone(records), i, i+1), true
		}
	}
	return record{}, nil, false
}

// putIn is records with the login of this place replaced by the one given. A record goes back to the file as the
// bytes it holds, so one that never came from a file is given its own here.
func putIn(records []record, wanted scope, address *url.URL, token string) []record {
	held := recordJSON{URL: address.String(), Token: token}
	if !wanted.isEverywhere() {
		directory := wanted.directory
		held.Scope = &directory
	}
	fresh := record{scope: wanted, address: address, token: token}
	// Marshalling strings and a pointer to one cannot fail: the values encoding/json refuses are ones no record holds.
	fresh.raw, _ = json.Marshal(held)
	if _, kept, found := takeOut(records, wanted); found {
		return append(kept, fresh)
	}
	return append(slices.Clone(records), fresh)
}

// globalRecord is the record that applies wherever ytrack is run from.
func globalRecord(records []record) (record, bool) {
	for _, held := range records {
		if held.scope.isEverywhere() {
			return held, true
		}
	}
	return record{}, false
}

// heldHere is chainTo for the directory ytrack was called in. The working directory is asked for only when a
// record names one, so a file of a single global record is read by a caller who has no usable PWD.
func heldHere(records []record, env []string) ([]record, *diag.Fault) {
	var dir string
	if slices.ContainsFunc(records, func(held record) bool { return !held.scope.isEverywhere() }) {
		resolved, reason := workingDirectory(env)
		if reason != "" {
			return nil, elsewhere(reason)
		}
		dir = resolved
	}
	return chainTo(records, dir), nil
}

// chainTo is the records that apply in dir, the nearest directory first and the global record last. A caller that
// has the directory already passes it here rather than through heldHere, which would resolve it a second time.
func chainTo(records []record, dir string) []record {
	var chain []record
	for _, held := range records {
		if !held.scope.isEverywhere() && held.scope.covers(dir) {
			chain = append(chain, held)
		}
	}
	// Every scope covering dir is a prefix of it, so the longest is the nearest, and no two records share one.
	slices.SortFunc(chain, func(a, b record) int { return len(b.scope.directory) - len(a.scope.directory) })
	if global, ok := globalRecord(records); ok {
		chain = append(chain, global)
	}
	return chain
}

// workingDirectory is the physical path of the directory ytrack was called in, or the reason there is none; what
// the caller wanted it for goes in front of that reason, so the callers word it. PWD is the only place internal/cli
// learns the directory from, and a runner that hands a child its own PWD (python and node do) leaves it naming
// another directory, so it is believed only while it names the directory relative names resolve against.
func workingDirectory(env []string) (dir, reason string) {
	stated := lookup(env, pwdVariable)
	if stated == "" {
		return "", pwdVariable + " is not set"
	}
	if !filepath.IsAbs(stated) {
		return "", fmt.Sprintf("%s %s is not an absolute path", pwdVariable, render.Quote(stated))
	}
	named, err := directory(stated)
	if err != nil {
		return "", fmt.Sprintf("%s %s cannot be read: %v", pwdVariable, render.Quote(stated), err)
	}
	here, err := directory(".")
	if err != nil {
		return "", fmt.Sprintf("the directory ytrack was called in cannot be read: %v", err)
	}
	if !os.SameFile(named, here) {
		return "", fmt.Sprintf("%s %s is not the directory ytrack was called in", pwdVariable, render.Quote(stated))
	}
	// Scopes are written and matched by the physical path, or a record made under one name of a directory would
	// not be found under another.
	physical, err := filepath.EvalSymlinks(stated)
	if err != nil {
		return "", fmt.Sprintf("%s %s cannot be read: %v", pwdVariable, render.Quote(stated), err)
	}
	return physical, ""
}

// The directory a relative name resolves against, reached by opening it: the standard library's own way of naming
// the working directory answers with PWD whenever PWD names it, which is the very thing being judged here.
func directory(path string) (fs.FileInfo, error) {
	opened, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer opened.Close()
	return opened.Stat()
}

// Which record applies cannot be told from another directory, and falling back on the global record would send the
// call to whichever instance that one names.
func elsewhere(reason string) *diag.Fault {
	message := "a login is saved for a directory, so which one applies depends on the directory of the call, and " + reason
	return &diag.Fault{Code: diag.BadUsage, Message: message}
}

// damaged names the file and nothing inside it: the caller cannot mend a record by hand, and the path is what they
// hand over or take away.
func damaged(path string) *diag.Fault {
	return &diag.Fault{
		Code:    diag.BadUsage,
		Message: "the file of saved logins is damaged",
		Details: []render.Pair{{Key: "file", Value: render.NewString(path)}},
	}
}

// barred is the operating system refusing the directory ytrack keeps its things in — the rights, a read-only
// filesystem, a .ytrack that is not a directory. That is the caller's machine, as an unset HOME is, so the refusal
// names the directory and the system's own words, and nothing of the file inside it.
func barred(path, message string, err error) *diag.Fault {
	return &diag.Fault{
		Code:    diag.Denied,
		Message: message + ": " + systemReason(err),
		Details: []render.Pair{{Key: "directory", Value: render.NewString(filepath.Dir(path))}},
	}
}

// systemReason is what the system said without the path it said it about, which names the file.
func systemReason(err error) string {
	var onPath *fs.PathError
	var onLink *os.LinkError
	switch {
	case errors.As(err, &onPath):
		return onPath.Err.Error()
	case errors.As(err, &onLink):
		return onLink.Err.Error()
	}
	return err.Error()
}
