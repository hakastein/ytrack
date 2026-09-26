package script

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

// The SDK reads an empty string as a part the call does not give, so a flag given empty is refused here.
func rejectEmpty(opts options, flag, because string) *diag.Fault {
	if !opts.given(flag) || opts.string(flag) != "" {
		return nil
	}
	return &diag.Fault{Code: youtrack.CodeBadUsage, Message: "--" + flag + " " + because}
}

func rejectNoQuery(opts options, carries, thing string) *diag.Fault {
	if opts.given(queryFlag) {
		return nil
	}
	message := fmt.Sprintf(`no --query was given: it carries %s, and --query "" finds every %s`, carries, thing)
	return &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
}

func requireFlag(opts options, flag, message string) *diag.Fault {
	if opts.given(flag) {
		return nil
	}
	return &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
}

// The SDK reads a limit of 0 as its own default page.
func checkPage(opts options) *diag.Fault {
	if limit := opts.int(limitFlag); limit < 1 {
		message := fmt.Sprintf("--limit %d: a page holds at least one record", limit)
		return &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
	}
	return nil
}

func pageOf(opts options) youtrack.Page {
	return youtrack.Page{Limit: opts.int(limitFlag), Skip: opts.int(skipFlag)}
}

const everyComment = "all"

func commentsOf(opts options) (youtrack.Comments, *diag.Fault) {
	text := opts.string(commentsFlag)
	if text == everyComment {
		return youtrack.AllComments(), nil
	}
	last, err := strconv.Atoi(text)
	var because string
	switch {
	case errors.Is(err, strconv.ErrRange):
		because = "is a larger number than there could ever be comments"
	case err != nil:
		because = "is neither " + everyComment + " nor a whole number of comments"
	case last < 0:
		because = "is negative, and a number of comments is not"
	default:
		return youtrack.LastComments(last), nil
	}
	message := fmt.Sprintf("--%s %s %s", commentsFlag, render.Quote(text), because)
	return youtrack.Comments{}, &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
}

const (
	queryFlag        = "query"
	fieldsFlag       = "fields"
	limitFlag        = "limit"
	skipFlag         = "skip"
	commentsFlag     = "comments"
	summaryFlag      = "summary"
	descriptionFlag  = "description"
	contentFlag      = "content"
	textFlag         = "text"
	fieldFlag        = "field"
	attributeFlag    = "attribute"
	clearFlag        = "clear"
	parentFlag       = "parent"
	nameFlag         = "name"
	visibleForFlag   = "visible-for"
	updateableByFlag = "updateable-by"
	taggableByFlag   = "taggable-by"
	ownedByFlag      = "owned-by"
	dateFlag         = "date"
	typeFlag         = "type"
	durationFlag     = "duration"
	categoryFlag     = "category"
)

const (
	emptyDescription = "is empty, and YouTrack keeps an empty description as none: leave the flag out to file the " +
		"issue with no description at all"
	emptyContent = "is empty, and YouTrack keeps empty content as none: leave the flag out to file the article " +
		"with no content at all"
	emptyParent  = "names no article: leave the flag out to file the article at the root of its project"
	emptyOwnedBy = "is empty, and YouTrack keeps no user under an empty login: it takes the login of the user the " +
		"tag belongs to"
	emptyWorkType = "names no type of work: the types an issue may be written against are the settings of its " +
		"project, printed by ytrack project show <code> under plugins"
	emptyWorkDate = "names no day: leave the flag out to log the time today"
)

func issueFieldWrites(filled, cleared []string) (writes []youtrack.FieldWrite, clearsDescription bool, fault *diag.Fault) {
	for _, flag := range filled {
		name, value, split := strings.Cut(flag, "=")
		if !split {
			message := fmt.Sprintf("--%s %s holds no =: a custom field is filled by writing its name, an = and the "+
				"value, as in --%s Type=Task", fieldFlag, render.Quote(flag), fieldFlag)
			return nil, false, &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
		}
		writes = append(writes, youtrack.FieldWrite{Name: name, Values: []string{value}})
	}
	for _, name := range cleared {
		if strings.EqualFold(name, descriptionFlag) {
			clearsDescription = true
			continue
		}
		writes = append(writes, youtrack.FieldWrite{Name: name, Clear: true})
	}
	return writes, clearsDescription, nil
}

func articleClears(cleared []string) (clearsContent, clearsParent bool, fault *diag.Fault) {
	for _, name := range cleared {
		switch {
		case strings.EqualFold(name, contentFlag):
			clearsContent = true
		case strings.EqualFold(name, parentFlag):
			clearsParent = true
		default:
			message := fmt.Sprintf("--%s %s names no part of an article a call may empty: it takes %s or %s",
				clearFlag, render.Quote(name), contentFlag, parentFlag)
			return false, false, &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
		}
	}
	return clearsContent, clearsParent, nil
}

func workItemAttributes(filled []string) ([]youtrack.AttributeWrite, *diag.Fault) {
	written := make([]youtrack.AttributeWrite, 0, len(filled))
	for _, flag := range filled {
		name, value, split := strings.Cut(flag, "=")
		if !split {
			message := fmt.Sprintf("--%s %s holds no =: an attribute is set by writing its name, an = and the value, "+
				"as in --%s 'Формат работы=ИИагент'", attributeFlag, render.Quote(flag), attributeFlag)
			return nil, &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
		}
		written = append(written, youtrack.AttributeWrite{Name: name, Value: value})
	}
	return written, nil
}

type workItemClears struct {
	text       bool
	workType   bool
	attributes []youtrack.AttributeWrite
}

func workItemClearsOf(cleared []string) (workItemClears, *diag.Fault) {
	var clears workItemClears
	for _, name := range cleared {
		switch {
		case strings.EqualFold(name, textFlag):
			clears.text = true
		case strings.EqualFold(name, typeFlag):
			clears.workType = true
		case strings.EqualFold(name, durationFlag), strings.EqualFold(name, dateFlag):
			message := fmt.Sprintf("--%s %s names a part every work item holds: YouTrack answers one of null with "+
				"Field cannot be null, so there is no way to empty it; --%s writes it afresh",
				clearFlag, render.Quote(name), strings.ToLower(name))
			return workItemClears{}, &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
		case name == "":
			message := fmt.Sprintf(`--%s "" names nothing to empty: it takes %s, %s or the name of an attribute`,
				clearFlag, typeFlag, textFlag)
			return workItemClears{}, &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
		default:
			clears.attributes = append(clears.attributes, youtrack.AttributeWrite{Name: name, Clear: true})
		}
	}
	return clears, nil
}

// A work item is whole minutes, and time.Duration holds fewer of them than the int32 YouTrack keeps.
const longestWorkItem = math.MaxInt64 / int64(time.Minute)

func parseDuration(text string) (time.Duration, *diag.Fault) {
	minutes, read := periodMinutes(text)
	switch {
	case !read:
		message := fmt.Sprintf("duration %s is no ISO 8601 period of hours and minutes, as in PT1H30M, PT90M or "+
			"PT0M: ytrack writes a work item as the minutes it comes to, and neither a day nor a week is a fixed "+
			"count of them — YouTrack reads P1D as the working day of the instance — while a second and a "+
			"fraction are no part of what a work item holds", render.Quote(text))
		return 0, &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
	case minutes > longestWorkItem:
		message := fmt.Sprintf("duration %s is longer than the %d minutes a work item may be written for",
			render.Quote(text), longestWorkItem)
		return 0, &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
	}
	return time.Duration(minutes) * time.Minute, nil
}

func periodMinutes(text string) (int64, bool) {
	rest, isPeriod := strings.CutPrefix(text, "PT")
	if !isPeriod {
		return 0, false
	}
	hours, rest, hoursGiven, read := countBefore(rest, 'H')
	if !read {
		return 0, false
	}
	minutes, rest, minutesGiven, read := countBefore(rest, 'M')
	if !read || rest != "" || (!hoursGiven && !minutesGiven) {
		return 0, false
	}
	return min(hours*60, pastLongest) + minutes, true
}

const pastLongest = longestWorkItem + 1

func countBefore(text string, mark byte) (count int64, rest string, given, read bool) {
	before, after, marked := strings.Cut(text, string(mark))
	if !marked {
		return 0, text, false, true
	}
	if before == "" {
		return 0, "", false, false
	}
	for _, digit := range []byte(before) {
		if digit < '0' || digit > '9' {
			return 0, "", false, false
		}
		count = min(count*10+int64(digit-'0'), pastLongest)
	}
	return count, after, true, true
}
