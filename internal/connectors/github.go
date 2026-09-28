package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mtfuller/agentworks/internal/router"
	"github.com/mtfuller/agentworks/internal/spec"
	"github.com/mtfuller/agentworks/internal/store"
)

type GitHub struct {
	source  spec.Source
	api     *apiClient
	pattern *regexp.Regexp
	owner   string
	repo    string
}

func NewGitHub(source spec.Source, token string, client *http.Client) (*GitHub, error) {
	base := source.GitHub.APIURL
	if base == "" {
		base = "https://api.github.com"
	}
	api, err := newAPIClient(base, client, map[string]string{"Authorization": "Bearer " + token, "X-GitHub-Api-Version": "2026-03-10", "Accept": "application/vnd.github+json"})
	if err != nil {
		return nil, err
	}
	pattern := source.GitHub.WorkItemPattern
	if pattern == "" {
		pattern = `[A-Z][A-Z0-9]+-[0-9]+`
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile GitHub work item pattern")
	}
	parts := strings.Split(source.GitHub.Repository, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("GitHub repository must be owner/name")
	}
	return &GitHub{source: source, api: api, pattern: compiled, owner: parts[0], repo: parts[1]}, nil
}

func (github *GitHub) Test(ctx context.Context) error {
	var repository struct {
		FullName string `json:"full_name"`
	}
	_, err := github.api.json(ctx, http.MethodGet, github.repoPath(), nil, nil, &repository)
	if err != nil {
		return err
	}
	if !strings.EqualFold(repository.FullName, github.source.GitHub.Repository) {
		return fmt.Errorf("GitHub repository response did not match configured repository")
	}
	return nil
}

type githubPull struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	State     string     `json:"state"`
	HTMLURL   string     `json:"html_url"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	MergedAt  *time.Time `json:"merged_at"`
	Head      struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

type githubComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"html_url"`
	IssueURL  string    `json:"issue_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

func (github *GitHub) Poll(ctx context.Context, cursor string, now time.Time) (Batch, error) {
	since := connectorSince(cursor, github.source.Poll.InitialLookback, now)
	pulls, reset, err := github.pulls(ctx, since)
	if err != nil {
		return Batch{}, err
	}
	byNumber := map[int]githubPull{}
	batch := Batch{Cursor: now.Add(-2 * time.Second).Format(time.RFC3339Nano), RateLimitResetAt: reset, Events: []Emission{}, WorkItems: []store.WorkItem{}, Outcomes: []store.Outcome{}}
	for _, pull := range pulls {
		byNumber[pull.Number] = pull
		item := github.workItem(pull, now)
		batch.WorkItems = append(batch.WorkItems, item)
		data, _ := json.Marshal(map[string]any{"repository": github.source.GitHub.Repository, "number": pull.Number, "url": pull.HTMLURL})
		batch.Outcomes = append(batch.Outcomes, store.Outcome{ID: stableID("out", github.source.Name, "pr-opened", fmt.Sprint(pull.Number), pull.CreatedAt.Format(time.RFC3339Nano)), Type: "github.pr.opened", Subject: github.subject(pull.Number), WorkItemID: item.ID, OccurredAt: pull.CreatedAt, Data: data})
		if pull.MergedAt != nil && !pull.MergedAt.Before(since) {
			batch.Outcomes = append(batch.Outcomes, store.Outcome{ID: stableID("out", github.source.Name, "pr-merged", fmt.Sprint(pull.Number), pull.MergedAt.Format(time.RFC3339Nano)), Type: "github.pr.merged", Subject: github.subject(pull.Number), WorkItemID: item.ID, OccurredAt: *pull.MergedAt, Data: data})
		}
		checks, rateReset, err := github.checks(ctx, pull.Head.SHA)
		if err != nil {
			return Batch{}, err
		}
		if rateReset.After(batch.RateLimitResetAt) {
			batch.RateLimitResetAt = rateReset
		}
		if outcome, ok := github.checkOutcome(pull, item, checks, now); ok {
			batch.Outcomes = append(batch.Outcomes, outcome)
		}
	}
	comments, rateReset, err := github.comments(ctx, since)
	if err != nil {
		return Batch{}, err
	}
	if rateReset.After(batch.RateLimitResetAt) {
		batch.RateLimitResetAt = rateReset
	}
	for _, comment := range comments {
		number := issueNumber(comment.IssueURL)
		pull, ok := byNumber[number]
		if !ok {
			pull, err = github.pull(ctx, number)
			if err != nil {
				var responseError *HTTPError
				if errors.As(err, &responseError) && responseError.Status == http.StatusNotFound {
					continue
				}
				return Batch{}, err
			}
			byNumber[number] = pull
		}
		item := github.workItem(pull, now)
		data, _ := json.Marshal(map[string]any{"repository": github.source.GitHub.Repository, "pull_number": number, "author": comment.User.Login, "comment": comment.Body, "url": comment.HTMLURL})
		raw, _ := json.Marshal(comment)
		batch.Events = append(batch.Events, Emission{Envelope: router.Envelope{ID: fmt.Sprintf("comment:%d:%s", comment.ID, comment.UpdatedAt.UTC().Format(time.RFC3339Nano)), Source: github.source.Name, Type: "github.pr.comment.created", Subject: github.subject(number), Time: comment.UpdatedAt, Data: data, RawData: raw}, WorkItem: item})
	}
	return batch, nil
}

type githubCheck struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	CompletedAt *time.Time `json:"completed_at"`
}

func (github *GitHub) pulls(ctx context.Context, since time.Time) ([]githubPull, time.Time, error) {
	values := []githubPull{}
	var reset time.Time
	for page := 1; page <= 10; page++ {
		query := url.Values{"state": {"all"}, "sort": {"updated"}, "direction": {"desc"}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}
		var response []githubPull
		header, err := github.api.json(ctx, http.MethodGet, github.repoPath()+"/pulls", query, nil, &response)
		if err != nil {
			return nil, time.Time{}, err
		}
		reset = githubReset(header, reset)
		stop := false
		for _, pull := range response {
			if pull.UpdatedAt.Before(since) {
				stop = true
				continue
			}
			values = append(values, pull)
		}
		if stop || len(response) < 100 {
			return values, reset, nil
		}
	}
	return nil, time.Time{}, fmt.Errorf("GitHub pull polling exceeded the 10-page safety limit")
}

func (github *GitHub) comments(ctx context.Context, since time.Time) ([]githubComment, time.Time, error) {
	values := []githubComment{}
	var reset time.Time
	for page := 1; page <= 10; page++ {
		query := url.Values{"sort": {"updated"}, "direction": {"asc"}, "since": {since.UTC().Format(time.RFC3339)}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}
		var response []githubComment
		header, err := github.api.json(ctx, http.MethodGet, github.repoPath()+"/issues/comments", query, nil, &response)
		if err != nil {
			return nil, time.Time{}, err
		}
		reset = githubReset(header, reset)
		values = append(values, response...)
		if len(response) < 100 {
			return values, reset, nil
		}
	}
	return nil, time.Time{}, fmt.Errorf("GitHub comment polling exceeded the 10-page safety limit")
}

func (github *GitHub) pull(ctx context.Context, number int) (githubPull, error) {
	var result githubPull
	_, err := github.api.json(ctx, http.MethodGet, github.repoPath()+"/pulls/"+strconv.Itoa(number), nil, nil, &result)
	return result, err
}

func (github *GitHub) checks(ctx context.Context, sha string) ([]githubCheck, time.Time, error) {
	if sha == "" {
		return []githubCheck{}, time.Time{}, nil
	}
	var response struct {
		CheckRuns []githubCheck `json:"check_runs"`
	}
	header, err := github.api.json(ctx, http.MethodGet, github.repoPath()+"/commits/"+url.PathEscape(sha)+"/check-runs", url.Values{"per_page": {"100"}, "filter": {"latest"}}, nil, &response)
	if err != nil {
		return nil, time.Time{}, err
	}
	return response.CheckRuns, githubReset(header, time.Time{}), nil
}

func (github *GitHub) checkOutcome(pull githubPull, item store.WorkItem, checks []githubCheck, now time.Time) (store.Outcome, bool) {
	if len(checks) == 0 {
		return store.Outcome{}, false
	}
	completed, successful := true, true
	occurred := pull.UpdatedAt
	dataChecks := make([]map[string]string, 0, len(checks))
	for _, check := range checks {
		if check.Status != "completed" {
			completed = false
		}
		if check.Conclusion != "success" && check.Conclusion != "neutral" && check.Conclusion != "skipped" {
			successful = false
		}
		if check.CompletedAt != nil && check.CompletedAt.After(occurred) {
			occurred = *check.CompletedAt
		}
		dataChecks = append(dataChecks, map[string]string{"name": check.Name, "status": check.Status, "conclusion": check.Conclusion})
	}
	if !completed {
		return store.Outcome{}, false
	}
	kind := "github.checks.failed"
	if successful {
		kind = "github.checks.passed"
	}
	data, _ := json.Marshal(map[string]any{"repository": github.source.GitHub.Repository, "pull_number": pull.Number, "head_sha": pull.Head.SHA, "checks": dataChecks})
	return store.Outcome{ID: stableID("out", github.source.Name, kind, fmt.Sprint(pull.Number), pull.Head.SHA), Type: kind, Subject: github.subject(pull.Number), WorkItemID: item.ID, OccurredAt: occurred, Data: data}, true
}

func (github *GitHub) workItem(pull githubPull, now time.Time) store.WorkItem {
	key := github.pattern.FindString(pull.Title + "\n" + pull.Body)
	kind, external := "jira", key
	if key == "" {
		kind, external = "github-pr", fmt.Sprintf("%s#%d", github.source.GitHub.Repository, pull.Number)
	}
	data, _ := json.Marshal(map[string]any{"github_pr": pull.HTMLURL, "repository": github.source.GitHub.Repository, "number": pull.Number})
	title, state := "", ""
	if kind == "github-pr" {
		title, state = pull.Title, pull.State
	}
	return store.WorkItem{ID: stableID("work", kind, external), Kind: kind, ExternalKey: external, Title: title, State: state, Data: data, CreatedAt: now, UpdatedAt: now}
}

func (github *GitHub) repoPath() string {
	return "/repos/" + url.PathEscape(github.owner) + "/" + url.PathEscape(github.repo)
}
func (github *GitHub) subject(number int) string {
	return fmt.Sprintf("github:%s:pull/%d", github.source.GitHub.Repository, number)
}
func githubReset(header http.Header, current time.Time) time.Time {
	if header.Get("X-RateLimit-Reset") == "" {
		return current
	}
	value := rateLimitReset(header, time.Now().UTC())
	if value.After(current) {
		return value
	}
	return current
}
func issueNumber(raw string) int {
	parsed, _ := url.Parse(raw)
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 {
		return 0
	}
	value, _ := strconv.Atoi(parts[len(parts)-1])
	return value
}
